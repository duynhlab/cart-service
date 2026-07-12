// Package v1 implements cart's internal gRPC server — the read-only east-west
// surface introduced by RFC-0015 (homelab ADR-021): checkout snapshots the
// user's cart at session creation via GetCart. Cart's write path deliberately
// stays on the browser REST API and the saga's tokenless internal ClearCart
// route; nothing here mutates state.
//
// The server is a thin adapter over the same logic layer as the HTTP
// handlers, so both transports return identical data.
package v1

import (
	"context"
	"math"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	cartv1 "github.com/duynhlab/pkg/proto/cart/v1"

	"github.com/duynhlab/cart-service/internal/core/domain"
)

// CartReader is the slice of the logic layer this server depends on
// (dependency inversion — *logicv1.CartService satisfies it).
type CartReader interface {
	GetCart(ctx context.Context, userID string) (*domain.Cart, error)
}

// Server implements cart.v1.CartService.
type Server struct {
	cartv1.UnimplementedCartServiceServer

	svc CartReader
}

// NewServer wires the gRPC adapter over the cart logic layer.
func NewServer(svc CartReader) *Server {
	return &Server{svc: svc}
}

// GetCart returns the user's current cart items. An unknown user or an empty
// cart yields an empty list, not an error — emptiness is a business condition
// the caller decides on (checkout answers 409 on an empty snapshot).
func (s *Server) GetCart(ctx context.Context, req *cartv1.GetCartRequest) (*cartv1.GetCartResponse, error) {
	if req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	cart, err := s.svc.GetCart(ctx, req.GetUserId())
	if err != nil {
		return nil, status.Error(codes.Internal, "get cart failed")
	}

	items := make([]*cartv1.CartItem, 0, len(cart.Items))
	for _, it := range cart.Items {
		qty := it.Quantity
		// Cart quantities are small positive ints (validated at add-to-cart);
		// clamp defensively so the int→int32 wire conversion cannot overflow.
		if qty > math.MaxInt32 {
			qty = math.MaxInt32
		}
		items = append(items, &cartv1.CartItem{
			ProductId:   it.ProductID,
			ProductName: it.ProductName,
			Quantity:    int32(qty), //nolint:gosec // clamped above
			// The catalog stores float dollars; the wire contract is int64
			// minor units. Round half-away-from-zero once, at this boundary.
			CartPriceMinor: int64(math.Round(it.ProductPrice * 100)),
		})
	}
	return &cartv1.GetCartResponse{Items: items}, nil
}
