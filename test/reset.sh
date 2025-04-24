#!/bin/bash

echo "🧹 Resetting system..."

# 1. Stop all containers
echo "🔄 Stopping services..."
docker-compose down

# 2. Clear PostgreSQL data
echo "🗑️ Clearing PostgreSQL data..."
docker-compose up -d postgres

# Wait until postgres is ready
echo "⏳ Waiting for PostgreSQL to be ready..."
until docker-compose exec postgres pg_isready > /dev/null 2>&1; do sleep 1; done

# Truncate database tables
docker-compose exec postgres psql -U postgres -d albumdb -c "TRUNCATE albums RESTART IDENTITY CASCADE;"
docker-compose exec postgres psql -U postgres -d albumdb -c "TRUNCATE inventory RESTART IDENTITY CASCADE;"
docker-compose exec postgres psql -U postgres -d albumdb -c "TRUNCATE processed_orders RESTART IDENTITY CASCADE;"
docker-compose exec postgres psql -U postgres -d albumdb -c "TRUNCATE orders RESTART IDENTITY CASCADE;" 2>/dev/null || echo "(orders table not found)"

# 3. Clear Jaeger traces (by restarting it if using in-memory or Badger)
echo "🧼 Resetting Jaeger (memory/badger)..."
docker-compose down
docker-compose up -d

echo "✅ System reset complete. All traces and data cleared."
