CREATE TABLE project_member (
    project_id UUID NOT NULL,
    user_id UUID NOT NULL,
    role VARCHAR(20) NOT NULL DEFAULT 'member',
    joined_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    PRIMARY KEY (project_id, user_id),

    CONSTRAINT fk_project_member_projects
        FOREIGN KEY (project_id)
        REFERENCES projects(internal_id)
        ON DELETE CASCADE,

    CONSTRAINT fk_project_member_user
        FOREIGN KEY (user_id)
        REFERENCES users(internal_id)
        ON DELETE CASCADE,

    CONSTRAINT chk_project_member_role
        CHECK (role IN ('admin', 'member'))
);

CREATE INDEX idx_project_member_user_id
    ON project_member(user_id);