package services

import (
	"fmt"
	"log"
	"time"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// PurchaseOrderService управляет заказами на закупку
type PurchaseOrderService struct {
	db          *gorm.DB
	stockService *StockService
}

// NewPurchaseOrderService создает новый экземпляр PurchaseOrderService
func NewPurchaseOrderService(db *gorm.DB, stockService *StockService) *PurchaseOrderService {
	return &PurchaseOrderService{
		db:          db,
		stockService: stockService,
	}
}

// ReceivedItem представляет полученный товар при обработке заказа
type ReceivedItem struct {
	OrderItemID string     `json:"order_item_id"` // ID позиции заказа
	Quantity    float64    `json:"quantity"`      // Фактически полученное количество
	ExpiryDate  *time.Time `json:"expiry_date"`   // Срок годности (если указан)
}

// GetPurchaseOrders возвращает список заказов на закупку с фильтрацией
func (s *PurchaseOrderService) GetPurchaseOrders(
	branchID string,
	status string,
	includeOverdue bool,
	limit int,
) ([]models.PurchaseOrder, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	query := s.db.Model(&models.PurchaseOrder{}).
		Preload("Supplier").
		Preload("Branch").
		Preload("Items").
		Preload("Items.Nomenclature").
		Preload("Invoice").
		Where("deleted_at IS NULL").
		Order("order_date DESC, created_at DESC").
		Limit(limit)

	if branchID != "" && branchID != "all" {
		query = query.Where("branch_id = ?", branchID)
	}

	if status != "" {
		query = query.Where("status = ?", status)
	}

	// Фильтрация просроченных заказов
	if includeOverdue {
		query = query.Where("expected_delivery_date < ?", time.Now()).
			Where("status IN ?", []models.PurchaseOrderStatus{
				models.PurchaseOrderStatusOrdered,
				models.PurchaseOrderStatusPartiallyReceived,
			})
	}

	var orders []models.PurchaseOrder
	if err := query.Find(&orders).Error; err != nil {
		return nil, fmt.Errorf("ошибка получения заказов: %w", err)
	}

	return orders, nil
}

// GetPurchaseOrder возвращает заказ по ID
func (s *PurchaseOrderService) GetPurchaseOrder(orderID string) (*models.PurchaseOrder, error) {
	var order models.PurchaseOrder
	if err := s.db.
		Preload("Supplier").
		Preload("Branch").
		Preload("Items").
		Preload("Items.Nomenclature").
		Preload("Invoice").
		First(&order, "id = ? AND deleted_at IS NULL", orderID).Error; err != nil {
		return nil, fmt.Errorf("заказ не найден: %w", err)
	}
	return &order, nil
}

// CreatePurchaseOrder создает новый заказ на закупку
func (s *PurchaseOrderService) CreatePurchaseOrder(order *models.PurchaseOrder) error {
	// Валидация
	if order.SupplierID == "" {
		return fmt.Errorf("не указан поставщик")
	}
	if order.BranchID == "" {
		return fmt.Errorf("не указан филиал")
	}
	if order.CreatedBy == "" {
		return fmt.Errorf("не указан создатель заказа")
	}
	if order.ExpectedDeliveryDate.IsZero() {
		return fmt.Errorf("не указана ожидаемая дата доставки")
	}
	if len(order.Items) == 0 {
		return fmt.Errorf("заказ должен содержать хотя бы одну позицию")
	}

	// Пересчитываем общую сумму
	order.TotalAmount = order.CalculateTotalAmount()

	// Сохраняем заказ и позиции в транзакции
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("❌ Транзакция откачена из-за panic: %v", r)
		}
	}()

	if err := tx.Create(order).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("ошибка создания заказа: %w", err)
	}

	// Сохраняем позиции
	for i := range order.Items {
		order.Items[i].PurchaseOrderID = order.ID
		if err := tx.Create(&order.Items[i]).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка создания позиции: %w", err)
		}
	}

	tx.Commit()
	log.Printf("✅ Создан заказ на закупку: %s (ID: %s)", order.OrderNumber, order.ID)
	return nil
}

// UpdatePurchaseOrder обновляет заказ (только для черновиков)
func (s *PurchaseOrderService) UpdatePurchaseOrder(orderID string, updates map[string]interface{}) (*models.PurchaseOrder, error) {
	var order models.PurchaseOrder
	if err := s.db.First(&order, "id = ? AND deleted_at IS NULL", orderID).Error; err != nil {
		return nil, fmt.Errorf("заказ не найден: %w", err)
	}

	// Проверяем, что заказ является черновиком
	if order.Status != models.PurchaseOrderStatusDraft {
		return nil, fmt.Errorf("можно обновлять только черновики (текущий статус: %s)", order.Status)
	}

	// Обновляем поля
	if err := s.db.Model(&order).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("ошибка обновления заказа: %w", err)
	}

	// Пересчитываем общую сумму, если изменились позиции
	if _, ok := updates["items"]; ok {
		if err := s.db.Preload("Items").First(&order, "id = ?", orderID).Error; err == nil {
			order.TotalAmount = order.CalculateTotalAmount()
			s.db.Model(&order).Update("total_amount", order.TotalAmount)
		}
	}

	return &order, nil
}

// SendPurchaseOrder отправляет заказ поставщику (draft → ordered)
func (s *PurchaseOrderService) SendPurchaseOrder(orderID string, approvedBy string) error {
	var order models.PurchaseOrder
	if err := s.db.Preload("Items").First(&order, "id = ? AND deleted_at IS NULL", orderID).Error; err != nil {
		return fmt.Errorf("заказ не найден: %w", err)
	}

	if order.Status != models.PurchaseOrderStatusDraft {
		return fmt.Errorf("можно отправить только черновик (текущий статус: %s)", order.Status)
	}

	if len(order.Items) == 0 {
		return fmt.Errorf("нельзя отправить заказ без позиций")
	}

	order.Status = models.PurchaseOrderStatusOrdered
	if approvedBy != "" {
		order.ApprovedBy = &approvedBy
	}

	if err := s.db.Save(&order).Error; err != nil {
		return fmt.Errorf("ошибка отправки заказа: %w", err)
	}

	log.Printf("✅ Заказ отправлен поставщику: %s (ID: %s)", order.OrderNumber, order.ID)
	return nil
}

// ReceivePurchaseOrder обрабатывает получение заказа складским работником
// Создает накладную-черновик для сверки бухгалтерией (НЕ оприходует товар автоматически)
func (s *PurchaseOrderService) ReceivePurchaseOrder(
	purchaseOrderID string,
	receivedItems []ReceivedItem,
	performedBy string,
) error {
	// 1. Загружаем заказ
	var order models.PurchaseOrder
	if err := s.db.
		Preload("Items").
		Preload("Items.Nomenclature").
		Preload("Supplier").
		Preload("Branch").
		First(&order, "id = ? AND deleted_at IS NULL", purchaseOrderID).Error; err != nil {
		return fmt.Errorf("заказ не найден: %w", err)
	}

	// 2. Проверяем статус заказа
	if order.IsCancelled() {
		return fmt.Errorf("нельзя получить отмененный заказ")
	}
	if order.IsReceived() {
		return fmt.Errorf("заказ уже получен полностью")
	}
	if order.Status == models.PurchaseOrderStatusDraft {
		return fmt.Errorf("нельзя получить черновик, сначала отправьте заказ поставщику")
	}

	// 3. Сначала обрабатываем каждую позицию заказа и собираем данные для накладной
	totalAmount := 0.0
	var invoiceItems []map[string]interface{}

	for _, receivedItem := range receivedItems {
		// Находим соответствующую позицию заказа
		var orderItem *models.PurchaseOrderItem
		for i := range order.Items {
			if order.Items[i].ID == receivedItem.OrderItemID {
				orderItem = &order.Items[i]
				break
			}
		}
		if orderItem == nil {
			return fmt.Errorf("позиция заказа не найдена: %s", receivedItem.OrderItemID)
		}

		// Проверяем, что полученное количество не превышает заказанное
		newReceivedQuantity := orderItem.ReceivedQuantity + receivedItem.Quantity
		if newReceivedQuantity > orderItem.OrderedQuantity {
			return fmt.Errorf("полученное количество (%.2f) превышает заказанное (%.2f) для позиции %s",
				newReceivedQuantity, orderItem.OrderedQuantity, orderItem.Nomenclature.Name)
		}

		// Обновляем полученное количество в позиции заказа
		orderItem.ReceivedQuantity = newReceivedQuantity
		orderItem.ReceivedTotalPrice = orderItem.ReceivedQuantity * orderItem.PurchasePricePerUnit

		// Рассчитываем сумму для накладной
		totalAmount += orderItem.ReceivedTotalPrice

		// Формируем item для накладной
		itemData := map[string]interface{}{
			"nomenclature_id": orderItem.NomenclatureID,
			"quantity":        receivedItem.Quantity,
			"unit":            orderItem.Unit,
			"price_per_unit":  orderItem.PurchasePricePerUnit,
			"total_price":     orderItem.ReceivedTotalPrice,
		}
		if receivedItem.ExpiryDate != nil {
			itemData["expiry_date"] = receivedItem.ExpiryDate.Format("2006-01-02")
		}
		invoiceItems = append(invoiceItems, itemData)
	}

	// 4. Создаем или обновляем входящую накладную (ЧЕРНОВИК для сверки бухгалтерией)
	var invoice *models.Invoice
	if order.InvoiceID != nil {
		// Обновляем существующую накладную (частичное получение)
		invoices, err := s.stockService.GetInvoices("", "", 1000)
		if err != nil {
			return fmt.Errorf("ошибка загрузки накладной: %w", err)
		}
		for i := range invoices {
			if invoices[i].ID == *order.InvoiceID {
				invoice = &invoices[i]
				break
			}
		}
		if invoice == nil {
			return fmt.Errorf("накладная не найдена: %s", *order.InvoiceID)
		}
		
		// Проверяем, что накладная еще не оприходована
		if invoice.Status != models.InvoiceStatusDraft {
			return fmt.Errorf("накладная уже оприходована, нельзя обновить")
		}

		// Обновляем сумму накладной
		_, err = s.stockService.UpdateInvoice(invoice.ID, map[string]interface{}{
			"total_amount": totalAmount,
			"notes":        fmt.Sprintf("Создана из заказа %s. Требуется сверка бухгалтерией перед оприходованием.", order.OrderNumber),
		})
		if err != nil {
			return fmt.Errorf("ошибка обновления накладной: %w", err)
		}
	} else {
		// Создаем новую накладную (ЧЕРНОВИК)
		invoiceNumber := fmt.Sprintf("INV-%s", order.OrderNumber)
		invoiceDate := time.Now().Format("2006-01-02")
		notes := fmt.Sprintf("Создана из заказа %s. Требуется сверка бухгалтерией перед оприходованием.", order.OrderNumber)
		
		createdInvoice, err := s.stockService.CreateInvoice(
			invoiceNumber,
			&order.SupplierID,
			order.BranchID,
			totalAmount,
			invoiceDate,
			false, // isPaidCash
			performedBy,
			notes,
			"internal", // source
			invoiceItems,
		)
		if err != nil {
			return fmt.Errorf("ошибка создания накладной: %w", err)
		}
		invoice = createdInvoice
		log.Printf("✅ Создана накладная-черновик: %s (ID: %s) для заказа %s. Требуется сверка бухгалтерией.", invoiceNumber, invoice.ID, order.OrderNumber)
	}

	// 6. Обновляем статус заказа
	allItemsReceived := true
	hasPartialItems := false
	for _, item := range order.Items {
		if !item.IsFullyReceived() {
			allItemsReceived = false
		}
		if item.IsPartiallyReceived() {
			hasPartialItems = true
		}
	}

	// Обновляем статус заказа
	if allItemsReceived {
		order.Status = models.PurchaseOrderStatusReceived
		now := time.Now()
		order.ActualDeliveryDate = &now
		log.Printf("✅ Заказ %s получен полностью (накладная создана, ожидает сверки бухгалтерией)", order.OrderNumber)
	} else if hasPartialItems {
		order.Status = models.PurchaseOrderStatusPartiallyReceived
		log.Printf("✅ Заказ %s получен частично (накладная создана, ожидает сверки бухгалтерией)", order.OrderNumber)
	}

	order.ReceivedBy = &performedBy
	order.InvoiceID = &invoice.ID

	// 7. Сохраняем изменения в транзакции
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
			log.Printf("❌ Транзакция откачена из-за panic: %v", r)
		}
	}()

	// Обновляем позиции заказа
	for _, item := range order.Items {
		if err := tx.Save(&item).Error; err != nil {
			tx.Rollback()
			return fmt.Errorf("ошибка обновления позиции: %w", err)
		}
	}

	// Обновляем заказ
	if err := tx.Save(&order).Error; err != nil {
		tx.Rollback()
		return fmt.Errorf("ошибка обновления заказа: %w", err)
	}

	tx.Commit()
	log.Printf("✅ Заказ %s обработан. Накладная %s создана и ожидает сверки бухгалтерией.", order.OrderNumber, invoice.Number)
	return nil
}

// CancelPurchaseOrder отменяет заказ
func (s *PurchaseOrderService) CancelPurchaseOrder(orderID string, reason string) error {
	var order models.PurchaseOrder
	if err := s.db.First(&order, "id = ? AND deleted_at IS NULL", orderID).Error; err != nil {
		return fmt.Errorf("заказ не найден: %w", err)
	}

	if order.IsReceived() {
		return fmt.Errorf("нельзя отменить полученный заказ")
	}

	order.Status = models.PurchaseOrderStatusCancelled
	if reason != "" {
		if order.Notes != "" {
			order.Notes += "\n\nПричина отмены: " + reason
		} else {
			order.Notes = "Причина отмены: " + reason
		}
	}

	if err := s.db.Save(&order).Error; err != nil {
		return fmt.Errorf("ошибка отмены заказа: %w", err)
	}

	log.Printf("✅ Заказ отменен: %s (ID: %s)", order.OrderNumber, order.ID)
	return nil
}

// GetOverdueOrders возвращает список просроченных заказов
func (s *PurchaseOrderService) GetOverdueOrders(branchID string) ([]models.PurchaseOrder, error) {
	var orders []models.PurchaseOrder

	query := s.db.Model(&models.PurchaseOrder{}).
		Preload("Supplier").
		Preload("Branch").
		Preload("Items").
		Preload("Items.Nomenclature").
		Where("expected_delivery_date < ?", time.Now()).
		Where("status IN ?", []models.PurchaseOrderStatus{
			models.PurchaseOrderStatusOrdered,
			models.PurchaseOrderStatusPartiallyReceived,
		}).
		Where("deleted_at IS NULL").
		Order("expected_delivery_date ASC")

	if branchID != "" && branchID != "all" {
		query = query.Where("branch_id = ?", branchID)
	}

	if err := query.Find(&orders).Error; err != nil {
		return nil, fmt.Errorf("ошибка получения просроченных заказов: %w", err)
	}

	return orders, nil
}

