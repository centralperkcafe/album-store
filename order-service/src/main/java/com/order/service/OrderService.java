package com.order.service;

import com.order.model.Order;

import java.util.List;

public interface OrderService {
    
    List<Order> getAllOrders();
    
    Order getOrderById(Long id);
    
    List<Order> getOrdersByUserId(String userId);
    
    Order createOrder(Order order);
    
} 