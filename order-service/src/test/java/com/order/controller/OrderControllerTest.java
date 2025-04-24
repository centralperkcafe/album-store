package com.order.controller;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.order.model.Order;
// import com.order.model.OrderStatus; // Remove assumed import
import com.order.service.OrderService;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.boot.test.mock.mockito.MockBean;
import org.springframework.http.MediaType;
import org.springframework.test.web.servlet.MockMvc;

import java.math.BigDecimal; // Use BigDecimal for price
import java.time.LocalDateTime;
import java.util.Collections;
import java.util.List;
import java.util.Optional;

import static org.mockito.ArgumentMatchers.*;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;
import static org.springframework.test.web.servlet.request.MockMvcRequestBuilders.*;
import static org.springframework.test.web.servlet.result.MockMvcResultMatchers.*;

@SpringBootTest // Loads the full application context (can be optimized if needed)
// Use MOCK environment to avoid starting a real server, good for controller-only tests
// @SpringBootTest(webEnvironment = SpringBootTest.WebEnvironment.MOCK) 
@AutoConfigureMockMvc // Configures MockMvc
class OrderControllerTest {

    @Autowired
    private MockMvc mockMvc; // Bean for performing requests

    @MockBean
    private OrderService orderService; // Mock the service dependency

    @Autowired
    private ObjectMapper objectMapper; // For converting objects to JSON

    private Order sampleOrder1;
    private Order sampleOrderInput;

    @BeforeEach
    void setUp() {
        // Initialize sample data used in multiple tests
        sampleOrder1 = new Order();
        sampleOrder1.setId(1L);
        sampleOrder1.setUserId("user123");
        sampleOrder1.setAlbumId("album456");
        sampleOrder1.setQuantity(2);
        sampleOrder1.setStatus("PENDING"); // Initial status is PENDING
        sampleOrder1.setCreatedAt(LocalDateTime.now());
        sampleOrder1.setUpdatedAt(LocalDateTime.now());

        sampleOrderInput = new Order(); // For POST/PUT requests (no ID)
        sampleOrderInput.setUserId("user123");
        sampleOrderInput.setAlbumId("album456");
        sampleOrderInput.setQuantity(2);
        // Status is set by @PrePersist
    }

    @Test
    void getAllOrders_shouldReturnListOfOrders_whenAdmin() throws Exception {
        // Arrange
        List<Order> orders = Collections.singletonList(sampleOrder1);
        when(orderService.getAllOrders()).thenReturn(orders);

        // Act & Assert
        mockMvc.perform(get("/api/orders")
                        .header("Client-Type", "admin") // Add admin header
                        .accept(MediaType.APPLICATION_JSON))
                .andExpect(status().isOk())
                .andExpect(content().contentType(MediaType.APPLICATION_JSON))
                .andExpect(jsonPath("$.size()").value(1))
                .andExpect(jsonPath("$[0].id").value(sampleOrder1.getId()))
                .andExpect(jsonPath("$[0].userId").value(sampleOrder1.getUserId()));

        verify(orderService).getAllOrders(); // Verify service method was called
    }

    @Test
    void getAllOrders_shouldReturnForbidden_whenNotAdmin() throws Exception {
        // Act & Assert
        mockMvc.perform(get("/api/orders")
                        .header("Client-Type", "user") // Use non-admin header
                        .accept(MediaType.APPLICATION_JSON))
                .andExpect(status().isForbidden());
    }

    @Test
    void getOrderById_whenOrderExistsAndAdmin_shouldReturnOrder() throws Exception {
        // Arrange
        long orderId = 1L;
        when(orderService.getOrderById(orderId)).thenReturn(sampleOrder1); // Return Order directly

        // Act & Assert
        mockMvc.perform(get("/api/orders/{id}", orderId)
                        .header("Client-Type", "admin") // Add admin header
                        .accept(MediaType.APPLICATION_JSON))
                .andExpect(status().isOk())
                .andExpect(content().contentType(MediaType.APPLICATION_JSON))
                .andExpect(jsonPath("$.id").value(sampleOrder1.getId()))
                .andExpect(jsonPath("$.status").value("PENDING")); // Assert initial status

        verify(orderService).getOrderById(orderId);
    }

    @Test
    void getOrderById_whenOrderExistsAndOwner_shouldReturnOrder() throws Exception {
        // Arrange
        long orderId = 1L;
        when(orderService.getOrderById(orderId)).thenReturn(sampleOrder1);

        // Act & Assert
        mockMvc.perform(get("/api/orders/{id}", orderId)
                        .header("Client-Type", "user")
                        .param("userId", sampleOrder1.getUserId()) // Add matching userId
                        .accept(MediaType.APPLICATION_JSON))
                .andExpect(status().isOk())
                .andExpect(jsonPath("$.id").value(sampleOrder1.getId()));

        verify(orderService).getOrderById(orderId);
    }

    @Test
    void getOrderById_whenOrderExistsAndNotOwner_shouldReturnForbidden() throws Exception {
        // Arrange
        long orderId = 1L;
        when(orderService.getOrderById(orderId)).thenReturn(sampleOrder1);

        // Act & Assert
        mockMvc.perform(get("/api/orders/{id}", orderId)
                        .header("Client-Type", "user")
                        .param("userId", "anotherUser") // Non-matching userId
                        .accept(MediaType.APPLICATION_JSON))
                .andExpect(status().isForbidden());

        verify(orderService).getOrderById(orderId);
    }

    @Test
    void getOrderById_whenOrderDoesNotExist_shouldReturnNotFound() throws Exception {
        // Arrange
        long orderId = 99L;
        when(orderService.getOrderById(orderId)).thenThrow(new javax.persistence.EntityNotFoundException()); // Throw exception

        // Act & Assert
        mockMvc.perform(get("/api/orders/{id}", orderId)
                        .header("Client-Type", "admin") // Need header even for not found
                        .accept(MediaType.APPLICATION_JSON))
                .andExpect(status().isNotFound());

        verify(orderService).getOrderById(orderId);
    }

    @Test
    void createOrder_whenUser_shouldReturnCreatedOrder() throws Exception {
        // Arrange
        // Simulate the service returning the created order with an ID and PENDING status
        Order createdOrder = new Order();
        createdOrder.setId(1L); // Service assigns the ID
        createdOrder.setUserId(sampleOrderInput.getUserId());
        createdOrder.setAlbumId(sampleOrderInput.getAlbumId());
        createdOrder.setQuantity(sampleOrderInput.getQuantity());
        createdOrder.setStatus("PENDING"); // Service initializes status
        createdOrder.setCreatedAt(LocalDateTime.now());
        createdOrder.setUpdatedAt(LocalDateTime.now());

        when(orderService.createOrder(any(Order.class))).thenReturn(createdOrder);

        // Act & Assert
        mockMvc.perform(post("/api/orders")
                        .header("Client-Type", "user") // Add user header
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(sampleOrderInput))
                        .accept(MediaType.APPLICATION_JSON))
                .andExpect(status().isCreated()) // Expect HTTP 201 Created
                .andExpect(content().contentType(MediaType.APPLICATION_JSON))
                .andExpect(jsonPath("$.id").value(createdOrder.getId()))
                .andExpect(jsonPath("$.status").value("PENDING")); // Assert initial status

        // Verify the service method was called (can use ArgumentCaptor for more detail)
        verify(orderService).createOrder(any(Order.class));
    }

    @Test
    void createOrder_whenAdmin_shouldReturnForbidden() throws Exception {
         // Act & Assert
        mockMvc.perform(post("/api/orders")
                        .header("Client-Type", "admin") // Use admin header
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(sampleOrderInput))
                        .accept(MediaType.APPLICATION_JSON))
                .andExpect(status().isForbidden());
    }

    @Test
    void createOrder_withInvalidInput_shouldReturnBadRequest() throws Exception {
        // Arrange
        Order invalidInput = new Order();
        invalidInput.setAlbumId(null); // Example: Assuming albumId is required
        invalidInput.setUserId("test");
        invalidInput.setQuantity(-1); // Example: Assuming quantity must be positive

        // Act & Assert
        mockMvc.perform(post("/api/orders")
                        .header("Client-Type", "user")
                        .contentType(MediaType.APPLICATION_JSON)
                        .content(objectMapper.writeValueAsString(invalidInput))
                        .accept(MediaType.APPLICATION_JSON))
                 // Depending on validation setup, this might be caught before the controller
                 // method or handled by it. Assuming standard Spring validation:
                .andExpect(status().isBadRequest());
    }

    // TODO: Add tests for other endpoints (PUT for status update, DELETE if applicable)
    // TODO: Add tests for different error scenarios (e.g., service layer exceptions)

} 