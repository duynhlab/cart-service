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
