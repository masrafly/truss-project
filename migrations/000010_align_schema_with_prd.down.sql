-- PERHATIAN: down akan GAGAL jika sudah ada baris project_members dengan role 'owner'
-- atau tasks.title lebih dari 100 karakter. Ubah/hapus data tersebut lebih dulu.
ALTER TABLE tasks ALTER COLUMN title TYPE VARCHAR(100);

ALTER TABLE project_members DROP CONSTRAINT chk_project_members_role;
ALTER TABLE project_members
    ADD CONSTRAINT chk_project_member_role
    CHECK (role IN ('admin', 'member'));
