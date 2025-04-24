package com.order.kafka;

import com.order.model.Order;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.kafka.core.KafkaTemplate;
import org.springframework.stereotype.Component;

import java.time.LocalDateTime;
import java.time.ZoneOffset;
// import java.time.format.DateTimeFormatter; // No longer needed if using default toString
import java.util.HashMap;
import java.util.Map;
// import java.util.UUID; // Not used

@Component
@RequiredArgsConstructor
@Slf4j
public class OrderProducer {

    private final KafkaTemplate<String, Object> kafkaTemplate;
    
    // Topic names
    private static final String ORDER_CREATED_TOPIC = "order-created";
    // Removed unused topics:
    // private static final String PAYMENT_PROCESSED_TOPIC = "payment-processed";
    // private static final String ORDER_CONFIRMATIONS_TOPIC = "order-confirmations";
    
    // Removed unused formatter:
    // private static final DateTimeFormatter ISO_UTC_FORMATTER = DateTimeFormatter.ISO_INSTANT;
    
    /**
     * Sends an order created event to Kafka. 
     * This event signifies that the order has been successfully created and persisted.
     */
    public void sendOrderCreatedEvent(Order order) {
        String orderId = order.getId().toString();
        
        // Construct the message payload without status
        Map<String, Object> message = new HashMap<>();
        message.put("orderId", orderId);
        message.put("userId", order.getUserId());
        message.put("albumId", order.getAlbumId());
        message.put("quantity", order.getQuantity());
        // Use default Instant toString() which is ISO-8601 UTC
        message.put("timestamp", LocalDateTime.now().toInstant(ZoneOffset.UTC).toString()); 
        
        log.info("Sending order created event to topic '{}': {}", ORDER_CREATED_TOPIC, message);
        kafkaTemplate.send(ORDER_CREATED_TOPIC, orderId, message);
    }
    
    // Removed sendPaymentProcessedEvent method
    // /**
    //  * Sends a payment processed event to Kafka
    //  */
    // public void sendPaymentProcessedEvent(Order order, boolean success) { ... }

    // Removed sendOrderConfirmedEvent method
    // /**
    //  * Sends an order confirmed event to Kafka for inventory processing.
    //  */
    // public void sendOrderConfirmedEvent(Order order) { ... }
} 