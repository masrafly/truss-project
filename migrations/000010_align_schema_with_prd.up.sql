-- PRD FR-04: project memiliki peran Owner (untuk mencegah penghapusan Owner terakhir).
ALTER TABLE project_members DROP CONSTRAINT chk_project_member_role;
ALTER TABLE project_members
    ADD CONSTRAINT chk_project_members_role
    CHECK (role IN ('owner', 'admin', 'member'));

-- PRD FR-05: judul task maksimal 200 karakter.
ALTER TABLE tasks ALTER COLUMN title TYPE VARCHAR(200);
