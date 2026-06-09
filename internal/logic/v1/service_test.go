package v1

import (
	"context"
	"errors"
	"testing"

	"github.com/duynhlab/cart-service/internal/core/domain"
)

// MockCartRepository is a configurable mock of domain.CartRepository.
// Each field overrides the default no-op behaviour for the matching method.
type MockCartRepository struct {
	findByUserIDFunc func(ctx context.Context, userID string) (*domain.Cart, error)
	getItemCountFunc func(ctx context.Context, userID string) (int, error)
	addItemFunc      func(ctx context.Context, userID string, item *domain.CartItem) error
	updateItemFunc   func(ctx context.Context, userID, itemID string, quantity int) error
	removeItemFunc   func(ctx context.Context, userID, itemID string) error
	clearFunc        func(ctx context.Context, userID string) error
}

func (m *MockCartRepository) FindByUserID(ctx context.Context, userID string) (*domain.Cart, error) {
	if m.findByUserIDFunc != nil {
		return m.findByUserIDFunc(ctx, userID)
	}
	return &domain.Cart{UserID: userID}, nil
}

func (m *MockCartRepository) GetItemCount(ctx context.Context, userID string) (int, error) {
	if m.getItemCountFunc != nil {
		return m.getItemCountFunc(ctx, userID)
	}
	return 0, nil
}

func (m *MockCartRepository) AddItem(ctx context.Context, userID string, item *domain.CartItem) error {
	if m.addItemFunc != nil {
		return m.addItemFunc(ctx, userID, item)
	}
	return nil
}

func (m *MockCartRepository) UpdateItem(ctx context.Context, userID, itemID string, quantity int) error {
	if m.updateItemFunc != nil {
		return m.updateItemFunc(ctx, userID, itemID, quantity)
	}
	return nil
}

func (m *MockCartRepository) RemoveItem(ctx context.Context, userID, itemID string) error {
	if m.removeItemFunc != nil {
		return m.removeItemFunc(ctx, userID, itemID)
	}
	return nil
}

func (m *MockCartRepository) Clear(ctx context.Context, userID string) error {
	if m.clearFunc != nil {
		return m.clearFunc(ctx, userID)
	}
	return nil
}

var errRepo = errors.New("repo failure")

func TestGetCart(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		repo      *MockCartRepository
		wantItems int
		wantErr   error
	}{
		{
			name: "empty cart",
			repo: &MockCartRepository{
				findByUserIDFunc: func(ctx context.Context, userID string) (*domain.Cart, error) {
					return &domain.Cart{UserID: userID, Items: []domain.CartItem{}}, nil
				},
			},
			wantItems: 0,
		},
		{
			name: "cart with items",
			repo: &MockCartRepository{
				findByUserIDFunc: func(ctx context.Context, userID string) (*domain.Cart, error) {
					return &domain.Cart{
						UserID: userID,
						Items:  []domain.CartItem{{ID: "i1"}, {ID: "i2"}},
					}, nil
				},
			},
			wantItems: 2,
		},
		{
			name: "repository error",
			repo: &MockCartRepository{
				findByUserIDFunc: func(ctx context.Context, userID string) (*domain.Cart, error) {
					return nil, errRepo
				},
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewCartService(tt.repo)

			cart, err := service.GetCart(ctx, "user1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetCart() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if len(cart.Items) != tt.wantItems {
				t.Fatalf("GetCart() items = %d, want %d", len(cart.Items), tt.wantItems)
			}
		})
	}
}

func TestGetCartCount(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		repo      *MockCartRepository
		wantCount int
		wantErr   error
	}{
		{
			name: "zero items",
			repo: &MockCartRepository{
				getItemCountFunc: func(ctx context.Context, userID string) (int, error) {
					return 0, nil
				},
			},
			wantCount: 0,
		},
		{
			name: "multiple items",
			repo: &MockCartRepository{
				getItemCountFunc: func(ctx context.Context, userID string) (int, error) {
					return 5, nil
				},
			},
			wantCount: 5,
		},
		{
			name: "repository error",
			repo: &MockCartRepository{
				getItemCountFunc: func(ctx context.Context, userID string) (int, error) {
					return 0, errRepo
				},
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := NewCartService(tt.repo)

			count, err := service.GetCartCount(ctx, "user1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetCartCount() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if count != tt.wantCount {
				t.Fatalf("GetCartCount() count = %d, want %d", count, tt.wantCount)
			}
		})
	}
}

func TestAddToCart(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		req     domain.AddToCartRequest
		repo    *MockCartRepository
		wantErr error
	}{
		{
			name: "valid item",
			req: domain.AddToCartRequest{
				ProductID:    "p1",
				ProductName:  "Product 1",
				ProductPrice: 100.0,
				Quantity:     1,
			},
		},
		{
			name: "zero quantity",
			req: domain.AddToCartRequest{
				ProductID:    "p1",
				ProductName:  "Product 1",
				ProductPrice: 100.0,
				Quantity:     0,
			},
			wantErr: ErrInvalidQuantity,
		},
		{
			name: "negative quantity",
			req: domain.AddToCartRequest{
				ProductID:    "p1",
				ProductName:  "Product 1",
				ProductPrice: 100.0,
				Quantity:     -3,
			},
			wantErr: ErrInvalidQuantity,
		},
		{
			name: "duplicate product propagates conflict",
			req: domain.AddToCartRequest{
				ProductID:    "p1",
				ProductName:  "Product 1",
				ProductPrice: 100.0,
				Quantity:     1,
			},
			repo: &MockCartRepository{
				addItemFunc: func(ctx context.Context, userID string, item *domain.CartItem) error {
					return domain.ErrConflict
				},
			},
			wantErr: domain.ErrConflict,
		},
		{
			name: "repository error",
			req: domain.AddToCartRequest{
				ProductID:    "p1",
				ProductName:  "Product 1",
				ProductPrice: 100.0,
				Quantity:     2,
			},
			repo: &MockCartRepository{
				addItemFunc: func(ctx context.Context, userID string, item *domain.CartItem) error {
					return errRepo
				},
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := tt.repo
			if repo == nil {
				repo = &MockCartRepository{}
			}
			service := NewCartService(repo)

			item, err := service.AddToCart(ctx, "user1", tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AddToCart() error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if item != nil {
					t.Fatalf("AddToCart() item = %v, want nil on error", item)
				}
				return
			}
			if item == nil {
				t.Fatal("AddToCart() item = nil, want non-nil")
			}
			if item.ProductID != tt.req.ProductID || item.Quantity != tt.req.Quantity {
				t.Fatalf("AddToCart() item = %+v, want product %q qty %d",
					item, tt.req.ProductID, tt.req.Quantity)
			}
		})
	}
}

func TestUpdateItemQuantity(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name     string
		quantity int
		repo     *MockCartRepository
		wantErr  error
	}{
		{
			name:     "valid update",
			quantity: 3,
		},
		{
			name:     "zero quantity",
			quantity: 0,
			wantErr:  ErrInvalidQuantity,
		},
		{
			name:     "negative quantity",
			quantity: -1,
			wantErr:  ErrInvalidQuantity,
		},
		{
			name:     "item not found maps to ErrCartItemNotFound",
			quantity: 2,
			repo: &MockCartRepository{
				updateItemFunc: func(ctx context.Context, userID, itemID string, quantity int) error {
					return domain.ErrNotFound
				},
			},
			wantErr: ErrCartItemNotFound,
		},
		{
			name:     "repository error propagates",
			quantity: 2,
			repo: &MockCartRepository{
				updateItemFunc: func(ctx context.Context, userID, itemID string, quantity int) error {
					return errRepo
				},
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := tt.repo
			if repo == nil {
				repo = &MockCartRepository{}
			}
			service := NewCartService(repo)

			err := service.UpdateItemQuantity(ctx, "user1", "item1", tt.quantity)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("UpdateItemQuantity() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestRemoveItem(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		repo    *MockCartRepository
		wantErr error
	}{
		{
			name: "valid remove",
		},
		{
			name: "item not found maps to ErrCartItemNotFound",
			repo: &MockCartRepository{
				removeItemFunc: func(ctx context.Context, userID, itemID string) error {
					return domain.ErrNotFound
				},
			},
			wantErr: ErrCartItemNotFound,
		},
		{
			name: "repository error propagates",
			repo: &MockCartRepository{
				removeItemFunc: func(ctx context.Context, userID, itemID string) error {
					return errRepo
				},
			},
			wantErr: errRepo,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := tt.repo
			if repo == nil {
				repo = &MockCartRepository{}
			}
			service := NewCartService(repo)

			err := service.RemoveItem(ctx, "user1", "item1")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RemoveItem() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestClearCart(t *testing.T) {
	ctx := context.Background()

	t.Run("success calls repository with user id", func(t *testing.T) {
		called := false
		var gotUserID string

		mockRepo := &MockCartRepository{
			clearFunc: func(ctx context.Context, userID string) error {
				called = true
				gotUserID = userID
				return nil
			},
		}
		service := NewCartService(mockRepo)

		if err := service.ClearCart(ctx, "user1"); err != nil {
			t.Fatalf("ClearCart() error = %v, want nil", err)
		}
		if !called {
			t.Fatal("expected repository Clear to be called")
		}
		if gotUserID != "user1" {
			t.Fatalf("ClearCart() userID = %q, want %q", gotUserID, "user1")
		}
	})

	t.Run("repository error propagates", func(t *testing.T) {
		mockRepo := &MockCartRepository{
			clearFunc: func(ctx context.Context, userID string) error {
				return errRepo
			},
		}
		service := NewCartService(mockRepo)

		if err := service.ClearCart(ctx, "user1"); !errors.Is(err, errRepo) {
			t.Fatalf("ClearCart() error = %v, want %v", err, errRepo)
		}
	})
}
