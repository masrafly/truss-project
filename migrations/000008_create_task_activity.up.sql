CREATE TABLE task_activity (
    internal_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL,
    actor_id UUID NOT NULL,
    activity_type VARCHAR(50) NOT NULL,
    old_value JSONB,
    new_value JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT fk_task_activity_tasks
        FOREIGN KEY (task_id)
        REFERENCES tasks(internal_id)
        ON DELETE CASCADE,

    CONSTRAINT fk_task_activity_actor
        FOREIGN KEY (actor_id)
        REFERENCES users(internal_id)
        ON DELETE RESTRICT
);

CREATE INDEX idx_task_activity_task_created
    ON task_activity(task_id, created_at);