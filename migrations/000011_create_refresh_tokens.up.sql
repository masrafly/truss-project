-- Refresh token: nilai asli TIDAK pernah disimpan, hanya hash SHA-256 (hex, 64 karakter).
-- family_id menautkan semua token hasil rotasi dari satu login, sehingga pemakaian ulang
-- token lama (tanda pencurian) dapat mencabut seluruh rantai.
CREATE TABLE refresh_tokens (
    internal_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    family_id UUID NOT NULL,
    token_hash CHAR(64) NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_refresh_tokens_hash UNIQUE (token_hash),

    CONSTRAINT fk_refresh_tokens_users
        FOREIGN KEY (user_id)
        REFERENCES users(internal_id)
        ON DELETE CASCADE
);

CREATE INDEX idx_refresh_tokens_user ON refresh_tokens(user_id);
CREATE INDEX idx_refresh_tokens_family ON refresh_tokens(family_id);
