package com.order.kafka;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.core.JsonProcessingException;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.order.model.Order;
import com.order.repository.OrderRepository;
import lombok.Data;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.kafka.annotation.KafkaListener;
import org.springframework.stereotype.Component;
import org.springframework.transaction.annotation.Transactional;

import javax.persistence.EntityNotFoundException;
import java.util.Optional;

@Component
@RequiredArgsConstructor
@Slf4j
public class OrderEventConsumer {

    private final OrderRepository orderRepository;
    private final ObjectMapper objectMapper; // For parsing JSON

    // Define constants for status
    private static final String STATUS_SUCCEEDED = "SUCCEEDED";
    private static final String STATUS_FAILED = "FAILED";

    // DTO for order-succeeded event payload (matching Go struct)
    @Data // Lombok annotation for getters, setters, etc.
    @JsonIgnoreProperties(ignoreUnknown = true) // Ignore fields not present in Go struct
    static class OrderSucceededPayload {
        private String orderId;
        // Timestamp is ignored for now, but could be added if needed
    }

    // DTO for order-failed event payload (matching Go struct)
    @Data
    @JsonIgnoreProperties(ignoreUnknown = true)
    static class OrderFailedPayload {
        private String orderId;
        private String reason; // Match Go's reason field
    }

    @KafkaListener(topics = "order-succeeded", groupId = "order-service-status-updater")
    @Transactional // Ensure database update is transactional
    public void handleOrderSucceeded(String message) {
        log.info("Received order-succeeded event: {}", message);
        try {
            OrderSucceededPayload payload = objectMapper.readValue(message, OrderSucceededPayload.class);
            updateOrderStatus(payload.getOrderId(), STATUS_SUCCEEDED, null);
        } catch (JsonProcessingException e) {
            log.error("Error parsing order-succeeded event JSON: {}", message, e);
            // Decide how to handle parsing errors (e.g., DLQ)
        } catch (Exception e) {
            log.error("Error processing order-succeeded event for message: {}", message, e);
            // Decide how to handle other processing errors
        }
    }

    @KafkaListener(topics = "order-failed", groupId = "order-service-status-updater")
    @Transactional
    public void handleOrderFailed(String message) {
        log.info("Received order-failed event: {}", message);
        try {
            OrderFailedPayload payload = objectMapper.readValue(message, OrderFailedPayload.class);
            updateOrderStatus(payload.getOrderId(), STATUS_FAILED, payload.getReason());
        } catch (JsonProcessingException e) {
            log.error("Error parsing order-failed event JSON: {}", message, e);
            // Decide how to handle parsing errors (e.g., DLQ)
        } catch (Exception e) {
            log.error("Error processing order-failed event for message: {}", message, e);
            // Decide how to handle other processing errors
        }
    }

    private void updateOrderStatus(String orderIdStr, String newStatus, String reason) {
        try {
            Long orderId = Long.parseLong(orderIdStr);
            Optional<Order> orderOptional = orderRepository.findById(orderId);

            if (orderOptional.isPresent()) {
                Order order = orderOptional.get();
                // Optional: Add check to prevent updating already finalized status?
                // if (!order.getStatus().equals("PENDING")) { ... }
                order.setStatus(newStatus);
                // Optionally store the failure reason if needed in the Order entity
                orderRepository.save(order);
                log.info("Successfully updated status for Order ID {} to {}", orderId, newStatus);
            } else {
                log.warn("Order with ID {} not found when trying to update status to {}. Event might be stale or order deleted.", orderId, newStatus);
                // Consider logging this to an alert system or specific log file
            }
        } catch (NumberFormatException e) {
            log.error("Invalid Order ID format received in event: {}", orderIdStr, e);
        }
    }
} 