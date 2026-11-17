-- Samakan nama tabel dengan konvensi jamak (users, workspaces, projects, tasks),
-- default GORM, PRD, dan docs/DATABASE.md.
-- Constraint FK/CHECK tidak di-rename (nama lama tetap valid dan tidak memengaruhi perilaku).

ALTER TABLE workspace_member RENAME TO workspace_members;
ALTER INDEX workspace_member_pkey RENAME TO workspace_members_pkey;
ALTER INDEX idx_workspace_member_user_id RENAME TO idx_workspace_members_user_id;

ALTER TABLE project_member RENAME TO project_members;
ALTER INDEX project_member_pkey RENAME TO project_members_pkey;
ALTER INDEX idx_project_member_user_id RENAME TO idx_project_members_user_id;

ALTER TABLE task_comment RENAME TO task_comments;
ALTER INDEX task_comment_pkey RENAME TO task_comments_pkey;
ALTER INDEX idx_task_comment_task_created RENAME TO idx_task_comments_task_created;

ALTER TABLE task_activity RENAME TO task_activities;
ALTER INDEX task_activity_pkey RENAME TO task_activities_pkey;
ALTER INDEX idx_task_activity_task_created RENAME TO idx_task_activities_task_created;
