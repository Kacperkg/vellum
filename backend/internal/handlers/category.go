package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/services"
)

type CategoryHandler struct {
	categoryService *services.CategoryService
}

func NewCategoryHandler(categoryService *services.CategoryService) *CategoryHandler {
	return &CategoryHandler{
		categoryService: categoryService,
	}
}

func (h *CategoryHandler) FindByID(c *gin.Context) {
	categoryID, ok := getIDParam(c)
	if !ok {
		return
	}

	response, err := h.categoryService.WithContext(c.Request.Context()).FindByID(categoryID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *CategoryHandler) ListAll(c *gin.Context) {
	response, err := h.categoryService.WithContext(c.Request.Context()).ListAll()
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}
