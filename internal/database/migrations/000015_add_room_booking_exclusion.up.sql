CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE room_bookings
ADD CONSTRAINT room_bookings_no_overlap
EXCLUDE USING gist (
    campus_id WITH =,
    room_id WITH =,
    tstzrange(
        (booking_date::timestamp + start_time) AT TIME ZONE 'UTC',
        (booking_date::timestamp + start_time + (duration_minutes * INTERVAL '1 minute')) AT TIME ZONE 'UTC',
        '[)'
    ) WITH &&
);
