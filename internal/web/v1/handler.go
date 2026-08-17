package v1

import (
	"context"
	"errors"
	"net/http"

	"github.com/duynhlab/cart-service/internal/core/domain"
	logicv1 "github.com/duynhlab/cart-service/internal/logic/v1"
	"github.com/duynhlab/pkg/httpx"
	"github.com/duynhlab/pkg/logger/zapx"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

// isQuantityValidationErr reports whether a request-binding error includes a
// failed Quantity field constraint, so an invalid-quantity add can be counted
// distinctly from other request-validation failures.
func isQuantityValidationErr(err error) bool {
	var ve validator.ValidationErrors
	if !errors.As(err, &ve) {
		return false
	}
	for _, fe := range ve {
		if fe.Field() == "Quantity" {
			return true
		}
	}
	return false
}

// CartHandler holds the cart service dependency
type CartHandler struct {
	cartService *logicv1.CartService
}

// NewCartHandler creates a new cart handler with dependency injection
func NewCartHandler(cartService *logicv1.CartService) *CartHandler {
	return &CartHandler{cartService: cartService}
}

// serverSpan returns the request context and the otelgin server span for this
// request. The web layer does not mint its own span — otelgin already opened
// the server span (method/route are on it), so handlers annotate that span via
// the returned handle. The caller must NOT end it; otelgin owns its lifecycle.
func serverSpan(c *gin.Context) (context.Context, trace.Span) {
	ctx := c.Request.Context()
	return ctx, trace.SpanFromContext(ctx)
}

func (h *CartHandler) GetCart(c *gin.Context) {
	ctx, span := serverSpan(c)

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
	ctx, span := serverSpan(c)

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
		// The quantity rule (min=1) is a binding constraint, so an invalid
		// quantity is rejected here, before logic. Count it as the business
		// rejection the items_added KPI tracks; other field failures are not.
		if isQuantityValidationErr(err) {
			logicv1.RecordItemAdded(ctx, logicv1.ItemsAddedResultRejected)
		}
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

	logicv1.RecordItemAdded(ctx, logicv1.ItemsAddedResultAdded)
	zapx.FromContext(ctx).Info("Item added to cart", zap.String("user_id", userID), zap.String("product_id", req.ProductID))
	c.JSON(http.StatusOK, gin.H{"message": "Item added to cart"})
}

func (h *CartHandler) GetCartCount(c *gin.Context) {
	ctx, span := serverSpan(c)

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
	ctx, span := serverSpan(c)

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
	ctx, span := serverSpan(c)

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
	h.clearCart(c, userID, logicv1.ClearSourceUserREST)
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
	h.clearCart(c, userID, logicv1.ClearSourceInternalSaga)
}

// clearCart empties userID's cart and writes the HTTP response. Shared by the
// private (JWT) and internal (by-path) clear endpoints, which differ only in how
// they resolve the user id and which source they attribute the clear to.
func (h *CartHandler) clearCart(c *gin.Context, userID, source string) {
	ctx, span := serverSpan(c)

	if err := h.cartService.ClearCart(ctx, userID); err != nil {
		span.RecordError(err)
		zapx.FromContext(ctx).Error("Failed to clear cart", zap.Error(err))
		httpx.RespondError(c, http.StatusInternalServerError, httpx.CodeInternal, "Internal server error")
		return
	}

	logicv1.RecordCartCleared(ctx, source)
	c.JSON(http.StatusOK, gin.H{"message": "Cart cleared"})
}

// Global state removed to comply with AGENTS.md dependency injection rules
