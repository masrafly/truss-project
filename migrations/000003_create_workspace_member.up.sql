CREATE TABLE workspace_member (
    workspace_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role VARCHAR(20) NOT NULL DEFAULT 'member',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (workspace_id, user_id),

    CONSTRAINT fk_workspace_member_workspace
        FOREIGN KEY (workspace_id)
        REFERENCES workspaces(internal_id)
        ON DELETE CASCADE,

    CONSTRAINT fk_workspace_member_user
        FOREIGN KEY (user_id)
        REFERENCES users(internal_id)
        ON DELETE CASCADE,

    CONSTRAINT chk_workspace_member_role
        CHECK (role IN ('owner', 'admin', 'member'))
);

CREATE INDEX idx_workspace_member_user_id
    ON workspace_member(user_id);