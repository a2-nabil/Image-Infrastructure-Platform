DROP INDEX IF EXISTS idx_images_lat_lng;
ALTER TABLE images
    DROP COLUMN IF EXISTS location_source,
    DROP COLUMN IF EXISTS altitude,
    DROP COLUMN IF EXISTS longitude,
    DROP COLUMN IF EXISTS latitude;
