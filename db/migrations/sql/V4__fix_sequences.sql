-- V4__fix_sequences.sql
-- Fix sequence desynchronization caused by seed data inserting explicit ids.
-- Without this, the first application INSERT (adding a not-yet-seeded product
-- to a cart) collides on the primary key because cart_items_id_seq still points
-- at 1 while the seeded rows already occupy higher ids.

-- Set the sequence for cart_items table to the max id
SELECT setval('cart_items_id_seq', (SELECT MAX(id) FROM cart_items));
