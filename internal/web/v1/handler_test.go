package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/duynhlab/cart-service/internal/core/domain"
	logicv1 "github.com/duynhlab/cart-service/internal/logic/v1"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockCartRepository is a mock implementation of domain.CartRepository
type MockCartRepository struct {
	mock.Mock
}

func (m *MockCartRepository) FindByUserID(ctx context.Context, userID string) (*domain.Cart, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*domain.Cart), args.Error(1)
}

func (m *MockCartRepository) GetItemCount(ctx context.Context, userID string) (int, error) {
	args := m.Called(ctx, userID)
	return args.Int(0), args.Error(1)
}

func (m *MockCartRepository) AddItem(ctx context.Context, userID string, item *domain.CartItem) error {
	args := m.Called(ctx, userID, item)
	return args.Error(0)
}

func (m *MockCartRepository) UpdateItem(ctx context.Context, userID, itemID string, quantity int) error {
	args := m.Called(ctx, userID, itemID, quantity)
	return args.Error(0)
}

func (m *MockCartRepository) RemoveItem(ctx context.Context, userID, itemID string) error {
	args := m.Called(ctx, userID, itemID)
	return args.Error(0)
}

func (m *MockCartRepository) Clear(ctx context.Context, userID string) error {
	args := m.Called(ctx, userID)
	return args.Error(0)
}

func TestGetCart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		expectedCart := &domain.Cart{
			UserID: "1",
			Items:  []domain.CartItem{{ProductID: "p1", Quantity: 2}},
		}

		mockRepo.On("FindByUserID", mock.Anything, "1").Return(expectedCart, nil)

		service := logicv1.NewCartService(mockRepo)
		handler := NewCartHandler(service)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/cart", nil)
		// Mock user_id in context (simulating AuthMiddleware)
		c.Set("user_id", "1")

		handler.GetCart(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRepo.AssertExpectations(t)
	})

	// OpaqueUUIDSubject pins that the handler treats user_id as an opaque
	// string: a Keycloak-style UUID subject (ADR-041/042) passes through to
	// the repository unchanged, with no numeric parsing.
	t.Run("OpaqueUUIDSubject", func(t *testing.T) {
		const aliceSub = "a11ce000-0000-4000-8000-000000000001"
		mockRepo := new(MockCartRepository)
		expectedCart := &domain.Cart{
			UserID: aliceSub,
			Items:  []domain.CartItem{{ProductID: "p1", Quantity: 2}},
		}

		mockRepo.On("FindByUserID", mock.Anything, aliceSub).Return(expectedCart, nil)

		service := logicv1.NewCartService(mockRepo)
		handler := NewCartHandler(service)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/cart", nil)
		c.Set("user_id", aliceSub)

		handler.GetCart(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRepo.AssertExpectations(t)
	})

	t.Run("NotFound", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("FindByUserID", mock.Anything, "1").Return(nil, logicv1.ErrCartNotFound)

		service := logicv1.NewCartService(mockRepo)
		handler := NewCartHandler(service)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/cart", nil)
		c.Set("user_id", "1")

		handler.GetCart(c)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockRepo.AssertExpectations(t)
	})
}

func TestAddToCart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		req := domain.AddToCartRequest{
			ProductID:    "p1",
			ProductName:  "Product 1",
			ProductPrice: 10.0,
			Quantity:     1,
		}

		// Expect AddItem to be called with correct arguments
		// Note: The service reconstructs the CartItem from the request, so we match on fields
		mockRepo.On("AddItem", mock.Anything, "1", mock.MatchedBy(func(item *domain.CartItem) bool {
			return item.ProductID == req.ProductID && item.Quantity == req.Quantity
		})).Return(nil)

		service := logicv1.NewCartService(mockRepo)
		handler := NewCartHandler(service)

		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/cart", bytes.NewBuffer(body))
		c.Set("user_id", "1")

		handler.AddToCart(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRepo.AssertExpectations(t)
	})

	t.Run("InvalidRequest", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		service := logicv1.NewCartService(mockRepo)
		handler := NewCartHandler(service)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/cart", bytes.NewBufferString("invalid json"))
		c.Set("user_id", "1")

		handler.AddToCart(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("ServiceError", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		req := domain.AddToCartRequest{
			ProductID:    "p1",
			ProductName:  "Product 1",
			ProductPrice: 10.0,
			Quantity:     1,
		}

		mockRepo.On("AddItem", mock.Anything, "1", mock.Anything).Return(errors.New("db error"))

		service := logicv1.NewCartService(mockRepo)
		handler := NewCartHandler(service)

		body, _ := json.Marshal(req)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/cart", bytes.NewBuffer(body))
		c.Set("user_id", "1")

		handler.AddToCart(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockRepo.AssertExpectations(t)
	})
}

// errorCode decodes the standard httpx error envelope and returns its "code".
func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return body.Code
}

// newTestContext builds a gin test context wired to the given handler input.
func newTestContext(method, path string, body []byte) (*httptest.ResponseRecorder, *gin.Context) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	if body == nil {
		c.Request = httptest.NewRequest(method, path, nil)
	} else {
		c.Request = httptest.NewRequest(method, path, bytes.NewBuffer(body))
	}
	return w, c
}

func TestGetCartUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := new(MockCartRepository)
	handler := NewCartHandler(logicv1.NewCartService(mockRepo))

	w, c := newTestContext("GET", "/cart", nil)
	// No user_id set in context.

	handler.GetCart(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "UNAUTHORIZED", errorCode(t, w))
}

func TestGetCartServiceError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := new(MockCartRepository)
	mockRepo.On("FindByUserID", mock.Anything, "1").Return(nil, errors.New("db error"))
	handler := NewCartHandler(logicv1.NewCartService(mockRepo))

	w, c := newTestContext("GET", "/cart", nil)
	c.Set("user_id", "1")

	handler.GetCart(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, "INTERNAL_ERROR", errorCode(t, w))
	mockRepo.AssertExpectations(t)
}

func TestAddToCartInvalidQuantity(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := new(MockCartRepository)
	handler := NewCartHandler(logicv1.NewCartService(mockRepo))

	req := domain.AddToCartRequest{ProductID: "p1", ProductName: "Product 1", ProductPrice: 10.0, Quantity: 0}
	body, _ := json.Marshal(req)
	w, c := newTestContext("POST", "/cart", body)
	c.Set("user_id", "1")

	handler.AddToCart(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, "VALIDATION_ERROR", errorCode(t, w))
}

func TestAddToCartUnauthorized(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockRepo := new(MockCartRepository)
	handler := NewCartHandler(logicv1.NewCartService(mockRepo))

	w, c := newTestContext("POST", "/cart", []byte("{}"))

	handler.AddToCart(c)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, "UNAUTHORIZED", errorCode(t, w))
}

func TestGetCartCount(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("GetItemCount", mock.Anything, "1").Return(3, nil)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("GET", "/cart/count", nil)
		c.Set("user_id", "1")

		handler.GetCartCount(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRepo.AssertExpectations(t)
	})

	t.Run("Unauthorized", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("GET", "/cart/count", nil)

		handler.GetCartCount(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, "UNAUTHORIZED", errorCode(t, w))
	})

	t.Run("ServiceError", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("GetItemCount", mock.Anything, "1").Return(0, errors.New("db error"))
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("GET", "/cart/count", nil)
		c.Set("user_id", "1")

		handler.GetCartCount(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "INTERNAL_ERROR", errorCode(t, w))
		mockRepo.AssertExpectations(t)
	})
}

func TestUpdateCartItem(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("UpdateItem", mock.Anything, "1", "item1", 2).Return(nil)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		body, _ := json.Marshal(map[string]int{"quantity": 2})
		w, c := newTestContext("PUT", "/cart/items/item1", body)
		c.Set("user_id", "1")
		c.Params = gin.Params{{Key: "itemId", Value: "item1"}}

		handler.UpdateCartItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRepo.AssertExpectations(t)
	})

	t.Run("Unauthorized", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("PUT", "/cart/items/item1", []byte("{}"))

		handler.UpdateCartItem(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, "UNAUTHORIZED", errorCode(t, w))
	})

	t.Run("InvalidRequest", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		// quantity below min=1 fails binding validation.
		body, _ := json.Marshal(map[string]int{"quantity": 0})
		w, c := newTestContext("PUT", "/cart/items/item1", body)
		c.Set("user_id", "1")
		c.Params = gin.Params{{Key: "itemId", Value: "item1"}}

		handler.UpdateCartItem(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "VALIDATION_ERROR", errorCode(t, w))
	})

	t.Run("ServiceError", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("UpdateItem", mock.Anything, "1", "item1", 2).Return(errors.New("db error"))
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		body, _ := json.Marshal(map[string]int{"quantity": 2})
		w, c := newTestContext("PUT", "/cart/items/item1", body)
		c.Set("user_id", "1")
		c.Params = gin.Params{{Key: "itemId", Value: "item1"}}

		handler.UpdateCartItem(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "INTERNAL_ERROR", errorCode(t, w))
		mockRepo.AssertExpectations(t)
	})
}

func TestRemoveCartItem(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("RemoveItem", mock.Anything, "1", "item1").Return(nil)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("DELETE", "/cart/items/item1", nil)
		c.Set("user_id", "1")
		c.Params = gin.Params{{Key: "itemId", Value: "item1"}}

		handler.RemoveCartItem(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRepo.AssertExpectations(t)
	})

	t.Run("Unauthorized", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("DELETE", "/cart/items/item1", nil)

		handler.RemoveCartItem(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, "UNAUTHORIZED", errorCode(t, w))
	})

	t.Run("ServiceError", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("RemoveItem", mock.Anything, "1", "item1").Return(errors.New("db error"))
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("DELETE", "/cart/items/item1", nil)
		c.Set("user_id", "1")
		c.Params = gin.Params{{Key: "itemId", Value: "item1"}}

		handler.RemoveCartItem(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "INTERNAL_ERROR", errorCode(t, w))
		mockRepo.AssertExpectations(t)
	})
}

func TestClearCart(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("Clear", mock.Anything, "1").Return(nil)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("DELETE", "/cart", nil)
		c.Set("user_id", "1")

		handler.ClearCart(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRepo.AssertExpectations(t)
	})

	t.Run("Unauthorized", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("DELETE", "/cart", nil)

		handler.ClearCart(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.Equal(t, "UNAUTHORIZED", errorCode(t, w))
	})

	t.Run("ServiceError", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("Clear", mock.Anything, "1").Return(errors.New("db error"))
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("DELETE", "/cart", nil)
		c.Set("user_id", "1")

		handler.ClearCart(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "INTERNAL_ERROR", errorCode(t, w))
		mockRepo.AssertExpectations(t)
	})
}

func TestClearCartByUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Success clears by path user id, no JWT", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("Clear", mock.Anything, "1").Return(nil)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("DELETE", "/cart/v1/internal/cart/1", nil)
		c.Params = gin.Params{{Key: "userId", Value: "1"}}

		handler.ClearCartByUserID(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockRepo.AssertExpectations(t)
	})

	t.Run("Missing user id -> 400, repo untouched", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("DELETE", "/cart/v1/internal/cart/", nil)
		// no Params set -> c.Param("userId") == ""

		handler.ClearCartByUserID(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Equal(t, "VALIDATION_ERROR", errorCode(t, w))
		mockRepo.AssertNotCalled(t, "Clear", mock.Anything, mock.Anything)
	})

	t.Run("Service error -> 500", func(t *testing.T) {
		mockRepo := new(MockCartRepository)
		mockRepo.On("Clear", mock.Anything, "1").Return(errors.New("db down"))
		handler := NewCartHandler(logicv1.NewCartService(mockRepo))

		w, c := newTestContext("DELETE", "/cart/v1/internal/cart/1", nil)
		c.Params = gin.Params{{Key: "userId", Value: "1"}}

		handler.ClearCartByUserID(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Equal(t, "INTERNAL_ERROR", errorCode(t, w))
		mockRepo.AssertExpectations(t)
	})
}
