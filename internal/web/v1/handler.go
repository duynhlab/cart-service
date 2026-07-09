package v1

import (
	"errors"
	"net/http"

	"github.com/duynhlab/pkg/httpx"
	"github.com/duynhlab/pkg/logger/zapx"
	"github.com/duynhlab/cart-service/internal/core/domain"
	logicv1 "github.com/duynhlab/cart-service/internal/logic/v1"
	"github.com/duynhlab/cart-service/middleware"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// CartHandler holds the cart service dependency
type CartHandler struct {
	cartService *logicv1.CartService
}

// NewCartHandler creates a new cart handler with dependency injection
func NewCartHandler(cartService *logicv1.CartService) *CartHandler {
	return &CartHandler{cartService: cartService}
}

func (h *CartHandler) GetCart(c *gin.Context) {
	ctx, span := middleware.StartSpan(c.Request.Context(), "http.request", trace.WithAttributes(
		attribute.String("layer", "web"),
		attribute.String("method", c.Request.Method),
		attribute.String("path", c.Request.URL.Path),
	))
	defer span.End()

	// user_id is set on the context by the JWT auth middleware (authmw).
	userID := c.GetString("user_id")
	if userID == "" {
		httpx.RespondError(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Unauthorized")
		return
	}

	cart, err := h.cartService.GetCart(ctx, userID)
	if err != nil {
		span.RecordError(err)
		zapx.FromContext(ctx).Error("Failed to get cart", zap.Error(err))

		switch {
		case errors.Is(err, logicv1.ErrCartNotFound):
			httpx.RespondError(c, http.StatusNotFound, httpx.CodeNotFound, "Cart not found")
		default:
			httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		}
		return
	}

	zapx.FromContext(ctx).Info("Cart retrieved", zap.String("user_id", userID))
	c.JSON(http.StatusOK, cart)
}

func (h *CartHandler) AddToCart(c *gin.Context) {
	ctx, span := middleware.StartSpan(c.Request.Context(), "http.request", trace.WithAttributes(
		attribute.String("layer", "web"),
		attribute.String("method", c.Request.Method),
		attribute.String("path", c.Request.URL.Path),
	))
	defer span.End()

	// user_id is set on the context by the JWT auth middleware (authmw).
	userID := c.GetString("user_id")
	if userID == "" {
		httpx.RespondError(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Unauthorized")
		return
	}

	var req domain.AddToCartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		span.SetAttributes(attribute.Bool("request.valid", false))
		span.RecordError(err)
		zapx.FromContext(ctx).Error("Invalid request", zap.Error(err))
		httpx.RespondError(c, http.StatusBadRequest, httpx.CodeValidation, err.Error())
		return
	}

	span.SetAttributes(attribute.Bool("request.valid", true))
	_, err := h.cartService.AddToCart(ctx, userID, req)
	if err != nil {
		span.RecordError(err)
		zapx.FromContext(ctx).Error("Failed to add to cart", zap.Error(err))

		switch {
		case errors.Is(err, logicv1.ErrInvalidQuantity):
			httpx.RespondError(c, http.StatusBadRequest, httpx.CodeValidation, "Invalid quantity")
		default:
			httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		}
		return
	}

	zapx.FromContext(ctx).Info("Item added to cart", zap.String("user_id", userID), zap.String("product_id", req.ProductID))
	c.JSON(http.StatusOK, gin.H{"message": "Item added to cart"})
}

func (h *CartHandler) GetCartCount(c *gin.Context) {
	ctx, span := middleware.StartSpan(c.Request.Context(), "http.request", trace.WithAttributes(
		attribute.String("layer", "web"),
		attribute.String("method", c.Request.Method),
		attribute.String("path", c.Request.URL.Path),
	))
	defer span.End()

	userID := c.GetString("user_id")
	if userID == "" {
		httpx.RespondError(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Unauthorized")
		return
	}

	count, err := h.cartService.GetCartCount(ctx, userID)
	if err != nil {
		span.RecordError(err)
		zapx.FromContext(ctx).Error("Failed to get cart count", zap.Error(err))
		httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		return
	}

	c.JSON(http.StatusOK, gin.H{"count": count})
}

func (h *CartHandler) UpdateCartItem(c *gin.Context) {
	ctx, span := middleware.StartSpan(c.Request.Context(), "http.request", trace.WithAttributes(
		attribute.String("layer", "web"),
		attribute.String("method", c.Request.Method),
		attribute.String("path", c.Request.URL.Path),
	))
	defer span.End()

	userID := c.GetString("user_id")
	if userID == "" {
		httpx.RespondError(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Unauthorized")
		return
	}

	itemID := c.Param("itemId")

	var req struct {
		Quantity int `json:"quantity" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		span.RecordError(err)
		zapx.FromContext(ctx).Error("Invalid request", zap.Error(err))
		httpx.RespondError(c, http.StatusBadRequest, httpx.CodeValidation, err.Error())
		return
	}

	err := h.cartService.UpdateItemQuantity(ctx, userID, itemID, req.Quantity)
	if err != nil {
		span.RecordError(err)
		zapx.FromContext(ctx).Error("Failed to update cart item", zap.Error(err))
		httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Cart item updated"})
}

func (h *CartHandler) RemoveCartItem(c *gin.Context) {
	ctx, span := middleware.StartSpan(c.Request.Context(), "http.request", trace.WithAttributes(
		attribute.String("layer", "web"),
		attribute.String("method", c.Request.Method),
		attribute.String("path", c.Request.URL.Path),
	))
	defer span.End()

	userID := c.GetString("user_id")
	if userID == "" {
		httpx.RespondError(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Unauthorized")
		return
	}

	itemID := c.Param("itemId")

	err := h.cartService.RemoveItem(ctx, userID, itemID)
	if err != nil {
		span.RecordError(err)
		zapx.FromContext(ctx).Error("Failed to remove cart item", zap.Error(err))
		httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Cart item removed"})
}

func (h *CartHandler) ClearCart(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		httpx.RespondError(c, http.StatusUnauthorized, httpx.CodeUnauthorized, "Unauthorized")
		return
	}
	h.clearCart(c, userID)
}

// ClearCartByUserID empties a cart by user id for in-cluster service-to-service
// callers (the order-fulfillment saga). It takes the user id from the path rather
// than a JWT, so no bearer token has to travel through the Temporal workflow
// input/history. Mounted only on the internal route group (never the gateway) and
// fenced by NetworkPolicy.
func (h *CartHandler) ClearCartByUserID(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		httpx.RespondError(c, http.StatusBadRequest, httpx.CodeValidation, "user_id required")
		return
	}
	h.clearCart(c, userID)
}

// clearCart empties userID's cart and writes the HTTP response. Shared by the
// private (JWT) and internal (by-path) clear endpoints, which differ only in how
// they resolve the user id.
func (h *CartHandler) clearCart(c *gin.Context, userID string) {
	ctx, span := middleware.StartSpan(c.Request.Context(), "http.request", trace.WithAttributes(
		attribute.String("layer", "web"),
		attribute.String("method", c.Request.Method),
		attribute.String("path", c.Request.URL.Path),
	))
	defer span.End()

	if err := h.cartService.ClearCart(ctx, userID); err != nil {
		span.RecordError(err)
		zapx.FromContext(ctx).Error("Failed to clear cart", zap.Error(err))
		httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Cart cleared"})
}

// Global state removed to comply with AGENTS.md dependency injection rules

