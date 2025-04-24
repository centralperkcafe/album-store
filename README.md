# Album Store MVP

## Project Overview

This is a microservice-based backend system for an online album store. The system enables browsing, inventory management, and ordering of music albums through a set of decoupled services.

## Architecture

The system is built using a microservice architecture, containing the following components:

- **Album Service**: Manages album metadata and catalog (Go)
- **Inventory Service**: Handles album inventory and stock management (Go)
- **Order Service**: Processes customer orders and payments (Java Spring Boot)
- **Kafka**: Message broker for asynchronous inter-service communication
- **PostgreSQL**: Database for persistent storage
- **OpenTelemetry**: Standards and tools for observability (distributed tracing)
- **Jaeger**: Backend system for collecting and visualizing traces
- **Docker**: For containerization of services and infrastructure

## Services and Ports

| Service           | Technology         | Port  | Description                                       |
| ----------------- | ------------------ | ----- | ------------------------------------------------- |
| Album Service     | Go                 | 8080  | Provides album information and catalog management |
| Inventory Service | Go                 | 8081  | Manages stock levels for albums                   |
| Order Service     | Java (Spring Boot) | 8082  | Processes customer orders                         |
| PostgreSQL        | -                  | 5432  | Database for all services                         |
| Kafka             | -                  | 9092  | Message broker                                    |
| Zookeeper         | -                  | 2181  | Kafka dependency                                  |
| **Jaeger UI**     | -                  | 16686 | Distributed Tracing UI                            |
| _(Jaeger OTLP)_   | -                  | 4317  | _(OTLP gRPC receiver)_                            |

## Getting Started

### Prerequisites

- Docker and Docker Compose v2.0+

### Running the System

1.  Clone the repository:
    ```bash
    git clone <repository-url>
    cd <repository-name>
    ```
2.  Build and run all services in detached mode:
    ```bash
    docker-compose up -d --build
    ```
3.  The services will be available at their respective ports listed above.
4.  To stop the services:
    ```bash
    docker-compose down
    ```
5.  To stop services and remove associated volumes (clears data):
    ```bash
    docker-compose down -v
    ```

## Observability (Distributed Tracing)

This system is instrumented using OpenTelemetry for distributed tracing. Traces are exported to Jaeger.

- **Access Jaeger UI:** Open your web browser and navigate to `http://localhost:16686`.
- You can select services (`order-service`, `album-service`, `inventory-service`) and view traces to understand request flow and diagnose issues.

## Load Testing

[K6](https://k6.io/) is the recommended tool for running load tests against this system.

1.  **Install K6:** Follow the [official K6 installation guide](https://k6.io/docs/getting-started/installation/).
2.  **Create Test Scripts:** Write your K6 test scripts (using JavaScript) in a directory (e.g., `loadtests/`). You can create different scripts for various scenarios like throughput testing, end-to-end latency testing, stress testing, etc. (Refer to previous conversation for test type suggestions).
3.  **Run Tests:** Execute your scripts from the project root directory using:
    ```bash
    # Example command (replace with your script path)
    k6 run loadtests/<your_script_name>.js
    ```
4.  **Analyze Results:**
    - Use the K6 output summary for key metrics (RPS, latency, error rates).
    - **Crucially, correlate K6 results with Jaeger traces.** Observe Jaeger UI during the test run to identify bottlenecks, errors, and high-latency operations within the microservices under  load.

## API Documentation

For detailed API documentation, see [API Documentation](./docs/api-docs.md).

## Message Flow

For details on the event-driven communication, see [Kafka Topics](./docs/kafka-topics.md).

## Client Types

The system supports two types of clients specified via the `Client-Type` HTTP header:

- **`user`**: Regular users who can browse albums and place orders.
- **`admin`**: Administrators who can manage albums, inventory, and potentially other administrative tasks (check API docs for specifics).

## Development

Each service can be developed independently. Refer to the individual service directories for specific development instructions.