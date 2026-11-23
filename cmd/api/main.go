// Composition root: satu-satunya tempat yang boleh mengenal semua lapisan
// dan menyambungkan implementasi konkret ke interface.
package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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
	"truss-project/internal/platform/health"
	"truss-project/internal/platform/logger"
	"truss-project/internal/platform/ratelimit"
	"truss-project/internal/platform/security"
)

func main() {
	// Mode pemeriksa kesehatan untuk Docker HEALTHCHECK (image tanpa curl/shell). Harus
	// sebelum config dibaca: tidak membutuhkan JWT_SECRET maupun database.
	if len(os.Args) > 1 && os.Args[1] == healthcheckCommand {
		os.Exit(healthcheckFromEnv())
	}

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

	if cfg.InsecureDBInProduction() {
		logg.Warn("koneksi database TANPA TLS di production (ALLOW_INSECURE_DB=true); " +
			"aman hanya bila database berada di jaringan tertutup yang sama")
	}
	if len(cfg.CORSAllowedOrigins) > 0 {
		logg.Info("CORS aktif", "origins", cfg.CORSAllowedOrigins)
	}

	db, err := database.Connect(cfg.DB, logg)
	if err != nil {
		fatal(logg, "gagal terhubung ke database", err)
	}
	logg.Info("database terhubung")

	// Infrastruktur
	hasher := security.NewBcryptHasher()
	jwtManager, err := security.NewJWTManager(cfg.JWTSecret, cfg.AccessTokenTTL)
	if err != nil {
		fatal(logg, "gagal membuat JWT manager", err)
	}

	// Modul auth: repository -> usecase -> handler
	userRepo := authrepo.NewUserGormRepository(db)
	// Batas login gagal per akun (email): 10 kali per 15 menit. Sengaja longgar karena
	// penyerang yang tahu email korban bisa memicu blokir sementara (lihat WithLoginThrottle).
	loginThrottle := ratelimit.New(10, 15*time.Minute)
	authUC := authusecase.NewAuthUseCase(userRepo, hasher, jwtManager, logg).
		WithLoginThrottle(loginThrottle).
		WithRefreshTokens(authrepo.NewRefreshTokenGormRepository(db), security.NewRefreshTokenCodec(), cfg.RefreshTokenTTL)
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
		middleware.SecurityHeaders(),
		middleware.RequestLogger(logg),
		middleware.Recovery(logg),
		// CORS sebelum autentikasi dan rate limit: preflight browser tidak membawa Authorization.
		middleware.CORS(cfg.CORSAllowedOrigins),
		middleware.BodyLimit(middleware.DefaultMaxBodyBytes),
	)

	// Endpoint health publik untuk container/orkestrator: /healthz (liveness) dan
	// /readyz (readiness, memeriksa database).
	sqlDB, err := db.DB()
	if err != nil {
		fatal(logg, "gagal mengambil koneksi database", err)
	}
	health.Register(router, sqlDB, logg)

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

	// Graceful shutdown: `docker stop` mengirim SIGTERM. Server berhenti menerima koneksi
	// baru dan memberi request yang sedang berjalan waktu untuk selesai, bukan memutusnya.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() { serverErr <- srv.ListenAndServe() }()

	logg.Info("server berjalan", "port", cfg.AppPort, "env", cfg.AppEnv,
		"trusted_proxies", len(cfg.TrustedProxies))

	select {
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal(logg, "gagal menjalankan server", err)
		}
	case <-ctx.Done():
		stop() // sinyal kedua langsung menghentikan proses (perilaku bawaan)
		logg.Info("sinyal berhenti diterima; shutdown dimulai", "batas_waktu", shutdownTimeout.String())

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			logg.Error("shutdown tidak tuntas; koneksi tersisa diputus", "error", err)
		}
		if err := sqlDB.Close(); err != nil {
			logg.Error("gagal menutup koneksi database", "error", err)
		}
		logg.Info("server berhenti")
	}
}

// shutdownTimeout: waktu maksimal menunggu request berjalan saat shutdown. Harus lebih kecil
// dari batas docker stop (default 10 detik) bila ingin tuntas sebelum SIGKILL; naikkan
// `stop_grace_period` di orkestrator jika memperbesar nilai ini.
const shutdownTimeout = 8 * time.Second

func fatal(l *slog.Logger, msg string, err error) {
	l.Error(msg, "error", err)
	os.Exit(1)
}
