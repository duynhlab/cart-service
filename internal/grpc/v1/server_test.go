package v1

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	cartv1 "github.com/duynhlab/pkg/proto/cart/v1"

	"github.com/duynhlab/cart-service/internal/core/domain"
)

// fakeCartReader is a configurable CartReader double.
type fakeCartReader struct {
	cart *domain.Cart
	err  error
}

func (f *fakeCartReader) GetCart(_ context.Context, _ string) (*domain.Cart, error) {
	return f.cart, f.err
}

func TestGetCart_MapsItemsToMinorUnits(t *testing.T) {
	s := NewServer(&fakeCartReader{cart: &domain.Cart{
		UserID: "7",
		Items: []domain.CartItem{
			{ProductID: "1", ProductName: "Wireless Mouse", ProductPrice: 29.99, Quantity: 2},
			{ProductID: "3", ProductName: "USB-C Hub", ProductPrice: 39.99, Quantity: 1},
		},
	}})

	resp, err := s.GetCart(context.Background(), &cartv1.GetCartRequest{UserId: "7"})
	if err != nil {
		t.Fatalf("GetCart() error = %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(resp.Items))
	}
	first := resp.Items[0]
	if first.ProductId != "1" || first.ProductName != "Wireless Mouse" || first.Quantity != 2 {
		t.Errorf("item[0] = %+v, want product 1 x2", first)
	}
	if first.CartPriceMinor != 2999 {
		t.Errorf("CartPriceMinor = %d, want 2999 (29.99 rounded to minor units)", first.CartPriceMinor)
	}
	if resp.Items[1].CartPriceMinor != 3999 {
		t.Errorf("item[1] CartPriceMinor = %d, want 3999", resp.Items[1].CartPriceMinor)
	}
}

func TestGetCart_EmptyCartIsEmptyListNotError(t *testing.T) {
	s := NewServer(&fakeCartReader{cart: &domain.Cart{UserID: "7"}})

	resp, err := s.GetCart(context.Background(), &cartv1.GetCartRequest{UserId: "7"})
	if err != nil {
		t.Fatalf("GetCart() error = %v, want nil (emptiness is the caller's business condition)", err)
	}
	if len(resp.Items) != 0 {
		t.Errorf("items = %d, want 0", len(resp.Items))
	}
}

func TestGetCart_MissingUserIDIsInvalidArgument(t *testing.T) {
	s := NewServer(&fakeCartReader{})

	_, err := s.GetCart(context.Background(), &cartv1.GetCartRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Errorf("code = %v, want InvalidArgument", status.Code(err))
	}
}

func TestGetCart_RepoErrorIsInternalWithoutLeak(t *testing.T) {
	s := NewServer(&fakeCartReader{err: errors.New("pq: connection refused to 10.0.0.5")})

	_, err := s.GetCart(context.Background(), &cartv1.GetCartRequest{UserId: "7"})
	st := status.Convert(err)
	if st.Code() != codes.Internal {
		t.Fatalf("code = %v, want Internal", st.Code())
	}
	if st.Message() != "get cart failed" {
		t.Errorf("message %q leaks internals, want opaque \"get cart failed\"", st.Message())
	}
}
