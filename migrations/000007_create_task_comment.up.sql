CREATE TABLE task_comment (
    internal_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL,
    user_id UUID NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_task_comment_task
        FOREIGN KEY (task_id)
        REFERENCES tasks(internal_id)
        ON DELETE CASCADE,

    CONSTRAINT fk_task_comment_user
        FOREIGN KEY (user_id)
        REFERENCES users(internal_id)
        ON DELETE RESTRICT
);

CREATE INDEX idx_task_comment_task_created
    ON task_comment(task_id, created_at);