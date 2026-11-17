// Composition root: satu-satunya tempat yang boleh mengenal semua lapisan
// dan menyambungkan implementasi konkret ke interface.
package main

import (
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	"truss-project/internal/config"
	"truss-project/internal/middleware"
	authhandler "truss-project/internal/modules/auth/handler"
	authrepo "truss-project/internal/modules/auth/repository"
	authusecase "truss-project/internal/modules/auth/usecase"
	projhandler "truss-project/internal/modules/projects/handler"
	projrepo "truss-project/internal/modules/projects/repository"
	projusecase "truss-project/internal/modules/projects/usecase"
	taskhandler "truss-project/internal/modules/tasks/handler"
	taskrepo "truss-project/internal/modules/tasks/repository"
	taskusecase "truss-project/internal/modules/tasks/usecase"
	wshandler "truss-project/internal/modules/workspaces/handler"
	wsrepo "truss-project/internal/modules/workspaces/repository"
	wsusecase "truss-project/internal/modules/workspaces/usecase"
	"truss-project/internal/platform/database"
	"truss-project/internal/platform/logger"
	"truss-project/internal/platform/ratelimit"
	"truss-project/internal/platform/security"
)

func main() {
	// .env opsional; logger belum ada di tahap ini, jadi pakai log standar.
	err := godotenv.Load()
	if err != nil {
		log.Println("File .env tidak ditemukan; menggunakan environment variables")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatal("Konfigurasi tidak valid: ", err)
	}

	// Logger dibuat pertama setelah config, lalu disuntikkan ke komponen lain.
	logg := logger.New(cfg.AppEnv, cfg.LogLevel)
	slog.SetDefault(logg)

	db, err := database.Connect(cfg.DB, logg)
	if err != nil {
		fatal(logg, "gagal terhubung ke database", err)
	}
	logg.Info("database terhubung")

	// Infrastruktur
	hasher := security.NewBcryptHasher()
	jwtManager, err := security.NewJWTManager(cfg.JWTSecret, 24*time.Hour)
	if err != nil {
		fatal(logg, "gagal membuat JWT manager", err)
	}

	// Modul auth: repository -> usecase -> handler
	userRepo := authrepo.NewUserGormRepository(db)
	// Batas login gagal per akun (email): 10 kali per 15 menit. Sengaja longgar karena
	// penyerang yang tahu email korban bisa memicu blokir sementara (lihat WithLoginThrottle).
	loginThrottle := ratelimit.New(10, 15*time.Minute)
	authUC := authusecase.NewAuthUseCase(userRepo, hasher, jwtManager, logg).WithLoginThrottle(loginThrottle)
	authHandler := authhandler.NewAuthHandler(authUC, logg)

	// Modul workspaces: repository -> usecase -> handler
	wsRepo := wsrepo.NewWorkspaceGormRepository(db)
	wsUC := wsusecase.NewWorkspaceUseCase(wsRepo, logg)
	wsHandler := wshandler.NewWorkspaceHandler(wsUC, logg)

	// Anggota workspace: repository yang sama, ditambah direktori user (port ke modul auth)
	userDirectory := wsrepo.NewUserDirectoryGorm(db)
	memberUC := wsusecase.NewMemberUseCase(wsRepo, userDirectory, logg)
	memberHandler := wshandler.NewMemberHandler(memberUC, logg)

	// Modul projects: repository -> usecase -> handler
	projectRepo := projrepo.NewProjectGormRepository(db)
	projectUC := projusecase.NewProjectUseCase(projectRepo, logg)
	projectHandler := projhandler.NewProjectHandler(projectUC, logg)
	projectMemberUC := projusecase.NewMemberUseCase(projectRepo, logg)
	projectMemberHandler := projhandler.NewMemberHandler(projectMemberUC, logg)

	// Modul tasks: repository -> usecase -> handler
	taskRepo := taskrepo.NewTaskGormRepository(db)
	taskUC := taskusecase.NewTaskUseCase(taskRepo, logg)
	taskHandler := taskhandler.NewTaskHandler(taskUC, logg)
	commentUC := taskusecase.NewCommentUseCase(taskRepo, logg)
	commentHandler := taskhandler.NewCommentHandler(commentUC, logg)

	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// gin.New() (bukan gin.Default()) supaya log & recovery memakai logger kita.
	router := gin.New()

	// Hanya proxy yang terdaftar yang boleh menentukan IP client (X-Forwarded-For).
	// Default kosong: IP client = alamat koneksi, sehingga tidak bisa dipalsukan.
	if err := router.SetTrustedProxies(cfg.TrustedProxies); err != nil {
		fatal(logg, "TRUSTED_PROXIES tidak valid", err)
	}

	router.Use(
		middleware.RequestID(),
		middleware.RequestLogger(logg),
		middleware.Recovery(logg),
		middleware.BodyLimit(middleware.DefaultMaxBodyBytes),
	)

	authMW := middleware.AuthMiddleware(jwtManager)
	// Batas per IP untuk register & login: 20 request per menit. Menahan banjir request
	// dari satu sumber, termasuk upaya membebani CPU lewat bcrypt.
	authIPLimiter := ratelimit.New(20, time.Minute)
	authHandler.RegisterRoutes(router, authMW, middleware.RateLimitByIP(authIPLimiter, "auth", logg))
	wsHandler.RegisterRoutes(router, authMW)
	memberHandler.RegisterRoutes(router, authMW)
	projectHandler.RegisterRoutes(router, authMW)
	projectMemberHandler.RegisterRoutes(router, authMW)
	taskHandler.RegisterRoutes(router, authMW)
	commentHandler.RegisterRoutes(router, authMW)

	// http.Server dengan timeout (router.Run tidak punya timeout sama sekali):
	// mencegah koneksi lambat (Slowloris) menahan server tanpa batas.
	srv := &http.Server{
		Addr:              ":" + cfg.AppPort,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	logg.Info("server berjalan", "port", cfg.AppPort, "env", cfg.AppEnv,
		"trusted_proxies", len(cfg.TrustedProxies))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fatal(logg, "gagal menjalankan server", err)
	}
}

func fatal(l *slog.Logger, msg string, err error) {
	l.Error(msg, "error", err)
	os.Exit(1)
}
