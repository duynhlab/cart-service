-- =============================================================================
-- Cart Service - Demo Seed Data (DEV ONLY)
-- =============================================================================
-- Purpose: Demo cart items for local/dev/demo environments only.
-- Applied ONLY by the `seed` subcommand -- NEVER by `migrate` or the serve
-- path, so production databases are never seeded with demo cart items.
-- Idempotent (ON CONFLICT DO NOTHING); safe to re-run.
-- Note: user_id is the OIDC token subject — the fixed Keycloak UUIDs of the
-- demo realm users (ADR-042). product_id references product.products.
-- =============================================================================

-- =============================================================================
-- CART ITEMS
-- =============================================================================
-- Alice's cart: 3 items (Wireless Mouse x2, Mechanical Keyboard x1, Webcam HD x1)
-- Bob's cart: 2 items (USB-C Hub x1, Laptop Stand x1)
-- Carol, David, Eve: No cart items (empty carts)

INSERT INTO cart_items (id, user_id, product_id, product_name, product_price, quantity, created_at, updated_at) VALUES
    -- Alice's cart
    (1, 'a11ce000-0000-4000-8000-000000000001', 1, 'Wireless Mouse', 29.99, 2, NOW() - INTERVAL '3 days', NOW() - INTERVAL '1 day'),   -- Wireless Mouse x2
    (2, 'a11ce000-0000-4000-8000-000000000001', 2, 'Mechanical Keyboard', 79.99, 1, NOW() - INTERVAL '2 days', NOW() - INTERVAL '2 days'),  -- Mechanical Keyboard x1
    (3, 'a11ce000-0000-4000-8000-000000000001', 5, 'Webcam HD', 69.99, 1, NOW() - INTERVAL '1 day', NOW() - INTERVAL '1 day'),    -- Webcam HD x1

    -- Bob's cart
    (4, 'a11ce000-0000-4000-8000-000000000002', 3, 'USB-C Hub', 49.99, 1, NOW() - INTERVAL '4 days', NOW() - INTERVAL '4 days'),  -- USB-C Hub x1
    (5, 'a11ce000-0000-4000-8000-000000000002', 4, 'Laptop Stand', 89.99, 1, NOW() - INTERVAL '3 days', NOW() - INTERVAL '3 days')   -- Laptop Stand x1
ON CONFLICT (user_id, product_id) DO NOTHING;

-- =============================================================================
-- VERIFICATION
-- =============================================================================
-- Verify seed data loaded
SELECT 
    'Cart items seeded' as status,
    COUNT(*) as cart_item_count,
    COUNT(DISTINCT user_id) as users_with_carts
FROM cart_items;

-- Sequence realignment (consolidated from the former 000004_fix_sequences):
-- the seed rows above use explicit ids, so realign the sequence to MAX(id) here,
-- or the first app INSERT collides on the primary key.
SELECT setval('cart_items_id_seq', (SELECT MAX(id) FROM cart_items));
