// Package controlplane is the Gin HTTP layer for the control-plane API.
// Handlers only decode requests, call a service, and encode responses: no
// SQL and no business rules live here. Each handler depends on a small
// interface, so it is tested with fakes.
package controlplane

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"apimarket/internal/domain"
	"apimarket/internal/domain/money"
	"apimarket/internal/service"
	mw "apimarket/internal/transport/http/middleware"
)

// ---------- ports (implemented by internal/service) ----------

type OrgCreator interface {
	Create(ctx context.Context, actor domain.Actor, in service.CreateOrgInput) (domain.Org, error)
}

type KeyManager interface {
	Create(ctx context.Context, actor domain.Actor, orgID, env, name string) (service.CreatedKey, error)
	Revoke(ctx context.Context, actor domain.Actor, keyID string) error
}

type Subscriber interface {
	Subscribe(ctx context.Context, actor domain.Actor, orgID, apiSlug, planID string) (domain.Subscription, error)
}

type Wallets interface {
	TopUp(ctx context.Context, actor domain.Actor, orgID string, rupees int64, paymentRef string) (string, money.Micro, error)
	Balance(ctx context.Context, orgID string) (money.Micro, error)
}

type CatalogLister interface {
	ListPublished(ctx context.Context) ([]service.CatalogEntry, error)
}

type Deps struct {
	Orgs       OrgCreator
	Keys       KeyManager
	Subs       Subscriber
	Wallets    Wallets
	Catalog    CatalogLister
	AdminToken string
	Log        *slog.Logger
}

// Router builds the control-plane Gin engine.
func Router(d Deps) *gin.Engine {
	h := handlers{d}
	r := gin.New()
	r.Use(mw.Recovery(d.Log), mw.RequestID(), mw.AccessLog(d.Log))
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	v1 := r.Group("/v1")
	v1.GET("/catalog/apis", h.listCatalog)

	admin := v1.Group("/admin", mw.BearerToken(d.AdminToken), func(c *gin.Context) {
		// Stage 0: a single platform-admin token. Google sign-in replaces this.
		c.Set(actorKey, domain.Actor{Type: "platform_admin", ID: "admin-token"})
		c.Next()
	})
	admin.POST("/orgs", h.createOrg)
	admin.POST("/orgs/:org/keys", h.createKey)
	admin.POST("/keys/:key/revoke", h.revokeKey)
	admin.POST("/orgs/:org/subscriptions", h.subscribe)
	admin.POST("/orgs/:org/topups", h.topUp)
	admin.GET("/orgs/:org/balance", h.balance)

	r.NoRoute(func(c *gin.Context) { mw.AbortWithError(c, http.StatusNotFound, "NOT_FOUND", "no such endpoint") })
	return r
}

const actorKey = "actor"

func actor(c *gin.Context) domain.Actor {
	a, _ := c.Get(actorKey)
	act, _ := a.(domain.Actor)
	return act
}

// fail maps service errors to HTTP. Unknown errors become 500 and are logged
// without leaking details to the client.
func fail(c *gin.Context, log *slog.Logger, err error) {
	if e, ok := domain.AsError(err); ok {
		status := map[domain.Kind]int{
			domain.KindInvalid: http.StatusBadRequest, domain.KindNotFound: http.StatusNotFound,
			domain.KindConflict: http.StatusConflict, domain.KindUnauthorized: http.StatusUnauthorized,
			domain.KindForbidden: http.StatusForbidden,
		}[e.Kind]
		if status != 0 {
			mw.AbortWithError(c, status, e.Code, e.Message)
			return
		}
	}
	if errors.Is(err, context.Canceled) {
		c.Abort()
		return
	}
	log.Error("request failed", "path", c.FullPath(), "err", err, "request_id", mw.GetRequestID(c))
	mw.AbortWithError(c, http.StatusInternalServerError, "INTERNAL", "internal error")
}

func badRequest(c *gin.Context, err error) {
	mw.AbortWithError(c, http.StatusBadRequest, "BAD_REQUEST", err.Error())
}
