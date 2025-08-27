package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/Caalamigeneral/internet-banking-backend/internal/config"
	"github.com/Caalamigeneral/internet-banking-backend/internal/database"
	"github.com/Caalamigeneral/internet-banking-backend/internal/handlers"
	"github.com/Caalamigeneral/internet-banking-backend/internal/middleware"
	"github.com/Caalamigeneral/internet-banking-backend/internal/repository"
	"github.com/Caalamigeneral/internet-banking-backend/internal/services"
)

func main() {
	// Load configuration
	cfg := config.Load()

	// Initialize database
	db, err := database.NewConnection(cfg.DatabaseURL)
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Auto-migrate tables
	if err := database.AutoMigrate(db); err != nil {
		log.Fatal("Failed to run migrations:", err)
	}

	// Initialize repositories
	userRepo := repository.NewUserRepository(db)
	transactionRepo := repository.NewTransactionRepository(db)
	auditRepo := repository.NewAuditRepository(db)

	// Initialize services
	authService := services.NewAuthService(userRepo, cfg.JWTSecret)
	transactionService := services.NewTransactionService(transactionRepo, auditRepo)

	// Initialize handlers
	authHandler := handlers.NewAuthHandler(authService)
	adminHandler := handlers.NewAdminHandler(transactionService)
	clientHandler := handlers.NewClientHandler(transactionService)

	// Setup router
	router := setupRouter(cfg, authHandler, adminHandler, clientHandler)

	// Setup server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start server
	go func() {
		log.Printf("🏦 Internet Banking Server starting on port %s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal("Server failed to start:", err)
		}
	}()

	// Wait for interrupt signal for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	log.Println("Server exited")
}

func setupRouter(cfg *config.Config, authHandler *handlers.AuthHandler, 
	adminHandler *handlers.AdminHandler, clientHandler *handlers.ClientHandler) *gin.Engine {
	
	gin.SetMode(cfg.GinMode)
	r := gin.New()

	// Global middleware
	r.Use(gin.Logger())
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.SecurityHeaders())

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"status":    "ok",
			"message":   "Internet Banking Backend is running! 🏦",
			"timestamp": time.Now(),
			"version":   "1.0.0",
		})
	})

	// Auth routes
	auth := r.Group("/api/v1/auth")
	{
		auth.POST("/login", authHandler.Login)
		auth.POST("/logout", middleware.AuthMiddleware(), authHandler.Logout)
		auth.POST("/refresh", authHandler.RefreshToken)
	}

	// Admin routes
	admin := r.Group("/api/v1/admin")
	admin.Use(middleware.AuthMiddleware())
	admin.Use(middleware.RoleMiddleware("admin", "super_admin"))
	{
		admin.GET("/dashboard", adminHandler.GetDashboard)
		admin.GET("/transactions", adminHandler.GetTransactions)
		admin.PUT("/transactions/:id/approve", adminHandler.ApproveTransaction)
		admin.PUT("/transactions/:id/reject", adminHandler.RejectTransaction)
	}

	// Client routes
	client := r.Group("/api/v1/client")
	client.Use(middleware.AuthMiddleware())
	{
		client.GET("/dashboard", clientHandler.GetDashboard)
		client.GET("/accounts", clientHandler.GetAccounts)
		client.POST("/payments/transfer", clientHandler.CreateTransfer)
		client.GET("/transactions", clientHandler.GetTransactions)
	}

	return r
}
