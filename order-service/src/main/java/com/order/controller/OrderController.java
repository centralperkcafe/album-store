package com.order.controller;

import com.order.model.Order;
import com.order.service.OrderService;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import javax.persistence.EntityNotFoundException;
import java.util.List;

@RestController
@RequestMapping("/api/orders")
@RequiredArgsConstructor
@Slf4j
public class OrderController {

    private final OrderService orderService;

    @GetMapping
    public ResponseEntity<List<Order>> getAllOrders(@RequestHeader("Client-Type") String clientType) {
        // Only admin can get all orders
        if (!"admin".equals(clientType)) {
            return ResponseEntity.status(HttpStatus.FORBIDDEN).build();
        }
        
        return ResponseEntity.ok(orderService.getAllOrders());
    }

    @GetMapping("/{id}")
    public ResponseEntity<Order> getOrderById(
            @PathVariable Long id,
            @RequestHeader("Client-Type") String clientType,
            @RequestParam(required = false) String userId) {
        
        try {
            Order order = orderService.getOrderById(id);
            
            // Only admin or the order owner can get a specific order
            if ("admin".equals(clientType) || (userId != null && userId.equals(order.getUserId()))) {
                return ResponseEntity.ok(order);
            } else {
                return ResponseEntity.status(HttpStatus.FORBIDDEN).build();
            }
        } catch (EntityNotFoundException e) {
            return ResponseEntity.notFound().build();
        }
    }

    @PostMapping
    public ResponseEntity<Order> createOrder(
            @RequestBody Order order,
            @RequestHeader("Client-Type") String clientType) {
        
        // Only users can create orders
        if (!"user".equals(clientType)) {
            return ResponseEntity.status(HttpStatus.FORBIDDEN).build();
        }
        
        // Logic inside createOrder in ServiceImpl handles Kafka event
        Order createdOrder = orderService.createOrder(order);
        return ResponseEntity.status(HttpStatus.CREATED).body(createdOrder);
    }
} 