-- Request replays are shared between replicas. No ticket or account data is copied.
CREATE TABLE portal_api_nonces (
 issuer text NOT NULL,
 nonce text NOT NULL,
 expires_at timestamptz NOT NULL,
 PRIMARY KEY (issuer,nonce)
);
CREATE INDEX portal_api_nonces_expiry ON portal_api_nonces(expires_at);
