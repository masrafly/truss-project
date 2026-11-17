package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

// Comment adalah komentar pada task (PRD FR-07).
type Comment struct {
	ID        uuid.UUID
	TaskID    uuid.UUID
	AuthorID  uuid.UUID
	Author    *UserRef // diisi saat dibaca
	Content   string
	CreatedAt time.Time
	UpdatedAt time.Time
}

var ErrCommentNotFound = errors.New("komentar tidak ditemukan")

// Izin komentar:
//
//	Baca & tulis : siapa pun yang dapat melihat task
//	Ubah         : hanya penulis (isi komentar adalah kata-kata penulis)
//	Hapus        : penulis, atau pengelola (moderasi)

func (a ProjectAccess) CanComment() bool {
	return a.CanView()
}

func (a ProjectAccess) CanEditComment(c Comment, actor uuid.UUID) bool {
	return a.CanView() && c.AuthorID == actor
}

func (a ProjectAccess) CanDeleteComment(c Comment, actor uuid.UUID) bool {
	return a.CanView() && (c.AuthorID == actor || a.IsManager())
}
