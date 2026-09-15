package routes

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/middleware"
)

type HandlerSet struct {
	Auth         *handlers.AuthHandler
	Accounts     *handlers.AccountHandler
	Categories   *handlers.CategoryHandler
	Budgets      *handlers.BudgetHandler
	Goals        *handlers.GoalHandler
	Transactions *handlers.TransactionHandler
	Settings     *handlers.UserSettingsHandler
}

func Register(router *gin.Engine, jwtSecret string, handlerSet HandlerSet) {
	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})
	router.POST("/register", handlerSet.Auth.Register)
	router.POST("/login", handlerSet.Auth.Login)
	router.POST("/refresh", handlerSet.Auth.Refresh)
	router.POST("/logout", handlerSet.Auth.Logout)

	api := router.Group("/api")
	api.Use(middleware.AuthMiddleware(jwtSecret))

	api.POST("/accounts", handlerSet.Accounts.Create)
	api.GET("/accounts", handlerSet.Accounts.ListByUser)
	api.GET("/accounts/:id", handlerSet.Accounts.GetByID)
	api.PUT("/accounts/:id", handlerSet.Accounts.Update)
	api.DELETE("/accounts/:id", handlerSet.Accounts.Delete)

	api.GET("/categories", handlerSet.Categories.ListAll)
	api.GET("/categories/:id", handlerSet.Categories.FindByID)

	api.POST("/budgets", handlerSet.Budgets.Create)
	api.GET("/budgets", handlerSet.Budgets.ListByUser)
	api.GET("/budgets/:id", handlerSet.Budgets.FindByID)
	api.PUT("/budgets/:id", handlerSet.Budgets.Update)
	api.DELETE("/budgets/:id", handlerSet.Budgets.Delete)

	api.POST("/goals", handlerSet.Goals.Create)
	api.GET("/goals", handlerSet.Goals.ListByUser)
	api.GET("/goals/:id", handlerSet.Goals.GetByID)
	api.PUT("/goals/:id", handlerSet.Goals.Update)
	api.DELETE("/goals/:id", handlerSet.Goals.Delete)

	api.POST("/transactions", handlerSet.Transactions.Create)
	api.GET("/transactions", handlerSet.Transactions.ListByUser)
	api.GET("/transactions/date-range", handlerSet.Transactions.ListByDateRange)
	api.GET("/transactions/frequency", handlerSet.Transactions.ListByFrequency)
	api.GET("/transactions/account/:account_id", handlerSet.Transactions.ListByAccount)
	api.GET("/transactions/category/:category_id", handlerSet.Transactions.ListByCategory)
	api.GET("/transactions/:id", handlerSet.Transactions.GetByID)
	api.PUT("/transactions/:id", handlerSet.Transactions.Update)
	api.DELETE("/transactions/:id", handlerSet.Transactions.Delete)

	api.GET("/settings", handlerSet.Settings.Get)
	api.PUT("/settings", handlerSet.Settings.Update)
}
