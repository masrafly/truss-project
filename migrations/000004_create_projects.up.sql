CREATE TABLE projects (
    internal_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    status VARCHAR(20) NOT NULL DEFAULT 'active',
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_projects_workspaces
        FOREIGN KEY (workspace_id)
        REFERENCES workspaces(internal_id)
        ON DELETE CASCADE,

    CONSTRAINT fk_projects_created_by
        FOREIGN KEY (created_by)
        REFERENCES users(internal_id)
        ON DELETE RESTRICT,

    CONSTRAINT chk_projects_status
        CHECK (status IN ('active', 'archived', 'completed'))
);

CREATE INDEX idx_projects_workspace_id
    ON projects(workspace_id);