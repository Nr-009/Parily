-- Drop existing foreign keys and re-add with ON DELETE CASCADE

-- files.room_id
ALTER TABLE files
    DROP CONSTRAINT IF EXISTS files_room_id_fkey,
    ADD CONSTRAINT files_room_id_fkey
        FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE;

-- room_members.room_id
ALTER TABLE room_members
    DROP CONSTRAINT IF EXISTS room_members_room_id_fkey,
    ADD CONSTRAINT room_members_room_id_fkey
        FOREIGN KEY (room_id) REFERENCES rooms(id) ON DELETE CASCADE;