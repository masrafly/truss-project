CREATE TABLE workspaces (
    internal_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(100) NOT NULL,
    description TEXT,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_workspaces_created_by
        FOREIGN KEY (created_by)
        REFERENCES users(internal_id)
        ON DELETE RESTRICT
);