-- Endpoint documentation for the seeded APIs (Stage 0 only). Requires seed.sql
-- and real-apis.sql. Safe to re-run: existing rows are refreshed.
-- Example responses are real responses captured from each service.

BEGIN;

INSERT INTO api_endpoints (api_id, method, path_template, idempotent, summary, description, params, headers,
                           request_example, response_status, response_content_type, response_example)
VALUES
-- Weather Now (mockprovider)
('00000000-0000-0000-0000-0000000000b1', 'GET', '/today', true,
 'Current weather for a city', 'Temperature and conditions right now.',
 '[{"name":"city","in":"query","required":false,"description":"City name; defaults to Bengaluru","example":"Mysuru"}]', '[]',
 '', 200, 'application/json',
 $j${"city":"Mysuru","condition":"partly cloudy","temp_c":24}$j$),

-- Echo Test API (mockprovider)
('00000000-0000-0000-0000-0000000000b2', 'POST', '/echo', false,
 'Echo the request back', 'Returns the method, path, query and body it received. Handy for checking your client.',
 '[]', '[{"name":"Content-Type","required":true,"description":"Body format","example":"application/json"}]',
 $j${"hello":"world"}$j$, 200, 'application/json',
 $j${"body":"{\"hello\":\"world\"}","method":"POST","path":"/echo","query":""}$j$),

-- Open-Meteo
('00000000-0000-0000-0000-0000000000b3', 'GET', '/forecast', true,
 'Weather forecast by coordinates', 'Current conditions and forecasts for any latitude and longitude.',
 '[{"name":"latitude","in":"query","required":true,"description":"Decimal degrees","example":"12.97"},
   {"name":"longitude","in":"query","required":true,"description":"Decimal degrees","example":"77.59"},
   {"name":"current","in":"query","required":false,"description":"Comma-separated variables to return now","example":"temperature_2m,wind_speed_10m"},
   {"name":"timezone","in":"query","required":false,"description":"IANA time zone for timestamps","example":"Asia/Kolkata"}]', '[]',
 '', 200, 'application/json',
 $j${"latitude":12.970123,"longitude":77.56364,"timezone":"Asia/Kolkata","current_units":{"time":"iso8601","temperature_2m":"°C","wind_speed_10m":"km/h"},"current":{"time":"2026-11-04T09:30","temperature_2m":24.8,"wind_speed_10m":2.2}}$j$),

-- IFSC Lookup
('00000000-0000-0000-0000-0000000000b4', 'GET', '/{ifsc}', true,
 'Bank branch by IFSC code', 'Branch name, address and supported payment rails (UPI, NEFT, RTGS, IMPS). Unknown codes return 404.',
 '[{"name":"ifsc","in":"path","required":true,"description":"11-character IFSC code","example":"HDFC0CAGSBK"}]', '[]',
 '', 200, 'application/json',
 $j${"BANK":"HDFC Bank","IFSC":"HDFC0CAGSBK","BRANCH":"THE AGS EMPLOYEES COOP BANK LTD","CITY":"BANGALORE URBAN","STATE":"KARNATAKA","UPI":true,"NEFT":true,"RTGS":true,"IMPS":true,"MICR":"560226263"}$j$),

-- JSONPlaceholder
('00000000-0000-0000-0000-0000000000b5', 'GET', '/posts/{id}', true,
 'Get a post', 'Fake data for prototyping.',
 '[{"name":"id","in":"path","required":true,"description":"Post id, 1 to 100","example":"1"}]', '[]',
 '', 200, 'application/json',
 $j${"userId":1,"id":1,"title":"sunt aut facere repellat provident occaecati excepturi optio reprehenderit","body":"quia et suscipit..."}$j$),
('00000000-0000-0000-0000-0000000000b5', 'POST', '/posts', false,
 'Create a post', 'The write is simulated: you get an id back but nothing is stored.',
 '[]', '[{"name":"Content-Type","required":true,"description":"Body format","example":"application/json"}]',
 $j${"title":"hello","body":"from the gateway","userId":1}$j$, 201, 'application/json',
 $j${"title":"hello","body":"from the gateway","userId":1,"id":101}$j$),

-- httpbin
('00000000-0000-0000-0000-0000000000b6', 'GET', '/headers', true,
 'Show the headers received', 'Useful to see exactly what reaches the provider.',
 '[]', '[]', '', 200, 'application/json',
 $j${"headers":{"Accept":"*/*","Host":"httpbin.org","User-Agent":"curl/8.7.1"}}$j$),
('00000000-0000-0000-0000-0000000000b6', 'GET', '/status/{code}', true,
 'Return a chosen status code', 'Test your error handling. Provider 5xx responses are not billed.',
 '[{"name":"code","in":"path","required":true,"description":"HTTP status to return","example":"503"}]', '[]',
 '', 503, 'text/html; charset=utf-8', '')
ON CONFLICT (api_id, method, path_template) DO UPDATE SET
  idempotent = EXCLUDED.idempotent, summary = EXCLUDED.summary, description = EXCLUDED.description,
  params = EXCLUDED.params, headers = EXCLUDED.headers, request_example = EXCLUDED.request_example,
  response_status = EXCLUDED.response_status, response_content_type = EXCLUDED.response_content_type,
  response_example = EXCLUDED.response_example;

COMMIT;
