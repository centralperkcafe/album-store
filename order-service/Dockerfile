FROM maven:3.8.5-openjdk-17 AS build
WORKDIR /app
COPY . .
RUN mvn clean package -DskipTests

FROM openjdk:17-slim
WORKDIR /app
COPY --from=build /app/target/*.jar app.jar

# Download OpenTelemetry Java Agent
ARG OTEL_AGENT_VERSION=2.0.0 
ADD https://github.com/open-telemetry/opentelemetry-java-instrumentation/releases/download/v${OTEL_AGENT_VERSION}/opentelemetry-javaagent.jar /app/opentelemetry-javaagent.jar

EXPOSE 8082
CMD ["java", "-javaagent:/app/opentelemetry-javaagent.jar", "-jar", "app.jar"]
