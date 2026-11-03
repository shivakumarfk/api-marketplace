package service

import (
	"context"
	"errors"

	"apimarket/internal/domain"
)

type KeyService struct {
	Tx    TxManager
	Orgs  OrgRepo
	Keys  KeyRepo
	Audit AuditRepo
	Gen   KeyGenerator
	Clock Clock
}

// CreatedKey carries the plain key, which is returned exactly once.
type CreatedKey struct {
	Key   domain.APIKey
	Plain string
}

func (s *KeyService) Create(ctx context.Context, actor domain.Actor, orgID, env, name string) (CreatedKey, error) {
	e, err := domain.ParseKeyEnv(env)
	if err != nil {
		return CreatedKey{}, err
	}
	gen, err := s.Gen.Generate(e)
	if err != nil {
		return CreatedKey{}, err
	}
	var out CreatedKey
	err = s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := getActiveOrg(ctx, s.Orgs, orgID); err != nil {
			return err
		}
		k, err := s.Keys.Create(ctx, domain.APIKey{
			OrgID: orgID, Env: e, Name: name, Prefix: gen.Prefix, Hash: gen.Hash, Status: "active",
		})
		if err != nil {
			return err
		}
		out = CreatedKey{Key: k, Plain: gen.Plain}
		return s.Audit.Record(ctx, audit(actor, orgID, "key.created", "api_key", k.ID,
			map[string]any{"env": string(e), "prefix": k.Prefix}))
	})
	return out, err
}

func (s *KeyService) Revoke(ctx context.Context, actor domain.Actor, keyID string) error {
	return s.Tx.WithinTx(ctx, func(ctx context.Context) error {
		orgID, err := s.Keys.Revoke(ctx, keyID, s.Clock.Now())
		if errors.Is(err, domain.ErrNotFound) {
			return domain.NotFound("KEY_NOT_FOUND", "active key not found")
		}
		if err != nil {
			return err
		}
		return s.Audit.Record(ctx, audit(actor, orgID, "key.revoked", "api_key", keyID, nil))
	})
}
