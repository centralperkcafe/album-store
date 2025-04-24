package com.order.service.impl;

import com.order.kafka.OrderProducer;
import com.order.model.Order;
import com.order.repository.OrderRepository;
import com.order.service.OrderService;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;

import javax.persistence.EntityNotFoundException;
import java.time.LocalDateTime;
import java.util.List;

@Service
@RequiredArgsConstructor
@Slf4j
public class OrderServiceImpl implements OrderService {

    private final OrderRepository orderRepository;
    private final OrderProducer orderProducer;

    @Override
    public List<Order> getAllOrders() {
        return orderRepository.findAll();
    }

    @Override
    public Order getOrderById(Long id) {
        return orderRepository.findById(id)
                .orElseThrow(() -> new EntityNotFoundException("Order not found with id: " + id));
    }

    @Override
    public List<Order> getOrdersByUserId(String userId) {
        return orderRepository.findByUserId(userId);
    }

    @Override
    public Order createOrder(Order order) {
        log.info("Creating order for user: {}, album: {}, quantity: {}", 
                order.getUserId(), order.getAlbumId(), order.getQuantity());
        
        Order savedOrder = orderRepository.save(order);
        
        // Publish order created event to Kafka
        orderProducer.sendOrderCreatedEvent(savedOrder);
        
        return savedOrder;
    }
} 