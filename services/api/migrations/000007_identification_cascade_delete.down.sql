ALTER TABLE image_identifications
    DROP CONSTRAINT IF EXISTS image_identifications_image_id_fkey;

ALTER TABLE image_identifications
    ADD CONSTRAINT image_identifications_image_id_fkey
    FOREIGN KEY (image_id) REFERENCES images(id) ON DELETE SET NULL;
