-- Роли и владелец товара.

CREATE TYPE user_role AS ENUM ('USER', 'SELLER', 'ADMIN');

ALTER TABLE users
    ADD COLUMN role user_role NOT NULL;

ALTER TABLE products
    ADD COLUMN seller_id UUID NOT NULL REFERENCES users (id);
