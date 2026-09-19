CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TABLE IF NOT EXISTS schema_benchmarks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    applied_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    version_tag VARCHAR(50) NOT NULL
);
