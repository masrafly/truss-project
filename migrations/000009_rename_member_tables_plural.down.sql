ALTER INDEX idx_task_activities_task_created RENAME TO idx_task_activity_task_created;
ALTER INDEX task_activities_pkey RENAME TO task_activity_pkey;
ALTER TABLE task_activities RENAME TO task_activity;

ALTER INDEX idx_task_comments_task_created RENAME TO idx_task_comment_task_created;
ALTER INDEX task_comments_pkey RENAME TO task_comment_pkey;
ALTER TABLE task_comments RENAME TO task_comment;

ALTER INDEX idx_project_members_user_id RENAME TO idx_project_member_user_id;
ALTER INDEX project_members_pkey RENAME TO project_member_pkey;
ALTER TABLE project_members RENAME TO project_member;

ALTER INDEX idx_workspace_members_user_id RENAME TO idx_workspace_member_user_id;
ALTER INDEX workspace_members_pkey RENAME TO workspace_member_pkey;
ALTER TABLE workspace_members RENAME TO workspace_member;
