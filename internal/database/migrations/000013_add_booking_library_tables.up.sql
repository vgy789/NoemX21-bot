CREATE TABLE rooms (
    id SMALLINT NOT NULL,
    campus_id UUID NOT NULL REFERENCES campuses(id),
    name VARCHAR(255) NOT NULL,
    min_duration INT NOT NULL DEFAULT 15,
    max_duration INT NOT NULL DEFAULT 120,
    is_active BOOLEAN DEFAULT true,
    description TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campus_id, id)
);

CREATE TABLE books (
    id SMALLINT NOT NULL,
    campus_id UUID NOT NULL REFERENCES campuses(id),
    title VARCHAR(255) NOT NULL,
    author VARCHAR(255) NOT NULL,
    category VARCHAR(255) NOT NULL,
    total_stock INT NOT NULL DEFAULT 1,
    description TEXT,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (campus_id, id)
);

CREATE TABLE room_bookings (
    id BIGSERIAL PRIMARY KEY,
    campus_id UUID NOT NULL REFERENCES campuses(id),
    room_id SMALLINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES user_accounts(id),
    booking_date DATE NOT NULL,
    start_time TIME NOT NULL,
    duration_minutes INT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (campus_id, room_id) REFERENCES rooms(campus_id, id),
    UNIQUE (campus_id, room_id, booking_date, start_time)
);

CREATE TABLE book_loans (
    id BIGSERIAL PRIMARY KEY,
    campus_id UUID NOT NULL REFERENCES campuses(id),
    book_id SMALLINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES user_accounts(id),
    borrowed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    due_at TIMESTAMP WITH TIME ZONE NOT NULL,
    returned_at TIMESTAMP WITH TIME ZONE,
    FOREIGN KEY (campus_id, book_id) REFERENCES books(campus_id, id)
);

CREATE INDEX idx_room_bookings_user_id ON room_bookings(user_id);
CREATE INDEX idx_room_bookings_date ON room_bookings(booking_date);
CREATE INDEX idx_book_loans_user_id ON book_loans(user_id);
CREATE INDEX idx_book_loans_active ON book_loans(book_id) WHERE returned_at IS NULL;
