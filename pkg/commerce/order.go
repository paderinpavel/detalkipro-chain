package commerce

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

type Order struct {
	ID        string `json:"order_id"`
	ItemCode  string `json:"item_code"`
	Quantity  int    `json:"quantity"`
	Timestamp int64  `json:"timestamp"`
}

type CommerceModule struct {
	mu       sync.Mutex
	orders   []Order
	baseDiff int
}

func NewCommerceModule(baseDiff int) *CommerceModule {
	return &CommerceModule{
		baseDiff: baseDiff,
	}
}

func (c *CommerceModule) RegisterOrder(order Order) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, o := range c.orders {
		if o.ID == order.ID {
			return c.baseDiff, fmt.Errorf("order already registered")
		}
	}

	c.orders = append(c.orders, order)
	newDiff := int(float64(c.baseDiff) * 0.8)
	if newDiff < 1 {
		newDiff = 1
	}
	return newDiff, nil
}

func (c *CommerceModule) GetCurrentDifficulty() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.baseDiff
}

func (c *CommerceModule) HandleOrderHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var order Order
	if err := json.NewDecoder(r.Body).Decode(&order); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	diff, err := c.RegisterOrder(order)
	if err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":           "ok",
		"order_id":         order.ID,
		"item_code":        order.ItemCode,
		"quantity":         order.Quantity,
		"new_difficulty":   diff,
	})
}
