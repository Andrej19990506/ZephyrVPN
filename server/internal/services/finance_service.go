package services

import (
	"fmt"
	"log"
	"time"

	"zephyrvpn/server/internal/models"

	"gorm.io/gorm"
)

// FinanceService управляет финансовыми транзакциями
type FinanceService struct {
	db *gorm.DB
}

// NewFinanceService создает новый экземпляр FinanceService
func NewFinanceService(db *gorm.DB) *FinanceService {
	return &FinanceService{db: db}
}

// CreateTransaction создает новую финансовую транзакцию
func (s *FinanceService) CreateTransaction(transaction *models.FinanceTransaction) error {
	if err := s.db.Create(transaction).Error; err != nil {
		return err
	}
	return nil
}

// GetTransactions получает список транзакций с фильтрацией
// Preload Counterparty для отображения реальных имен контрагентов
func (s *FinanceService) GetTransactions(branchID, source, entityIDs string) ([]models.FinanceTransaction, error) {
	var transactions []models.FinanceTransaction
	query := s.db.Model(&models.FinanceTransaction{}).
		Preload("Counterparty") // Загружаем данные контрагента для отображения имени

	if branchID != "" {
		query = query.Where("branch_id = ?", branchID)
	}

	if source != "" {
		query = query.Where("source = ?", source)
	}

	if entityIDs != "" {
		// TODO: Парсинг JSON массива entityIDs и фильтрация
		// Пока оставляем без фильтрации по entity_ids
	}

	if err := query.Order("date DESC, created_at DESC").Find(&transactions).Error; err != nil {
		return nil, err
	}

	return transactions, nil
}

// GetTransactionByID получает транзакцию по ID
func (s *FinanceService) GetTransactionByID(id string) (*models.FinanceTransaction, error) {
	var transaction models.FinanceTransaction
	if err := s.db.Preload("Counterparty").First(&transaction, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &transaction, nil
}

// CreateExpenseFromInvoice создает запись расхода (Expense) из накладной
// invoiceID: ID накладной
// counterpartyID: ID контрагента
// amount: сумма накладной
// branchID: ID филиала
// date: дата накладной
// isPaidCash: true если оплачено наличными
// performedBy: кто обработал накладную
func (s *FinanceService) CreateExpenseFromInvoice(
	invoiceID string,
	counterpartyID string,
	amount float64,
	branchID string,
	date time.Time,
	isPaidCash bool,
	performedBy string,
) (*models.FinanceTransaction, error) {
	source := models.TransactionSourceBank
	if isPaidCash {
		source = models.TransactionSourceCash
	}

	status := models.TransactionStatusPending
	if isPaidCash {
		// Наличные операции сразу Completed
		status = models.TransactionStatusCompleted
	}

	transaction := &models.FinanceTransaction{
		Date:          date,
		Type:          models.TransactionTypeExpense,
		Category:      "Операционные расходы",
		Amount:        amount,
		Description:   fmt.Sprintf("Оприходование накладной %s", invoiceID),
		BranchID:      branchID,
		Source:        source,
		Status:        status,
		CounterpartyID: &counterpartyID,
		InvoiceID:     &invoiceID,
		PerformedBy:   performedBy,
	}

	if err := s.db.Create(transaction).Error; err != nil {
		return nil, fmt.Errorf("ошибка создания финансовой транзакции: %v", err)
	}

	log.Printf("✅ Создана финансовая транзакция (Expense) для накладной %s: сумма=%.2f, источник=%s, статус=%s",
		invoiceID, amount, source, status)

	return transaction, nil
}

// GetCounterpartiesWithBalances получает список контрагентов с рассчитанными балансами из finance_transactions
// Использует агрегацию для избежания N+1 проблем
func (s *FinanceService) GetCounterpartiesWithBalances() ([]map[string]interface{}, error) {
	var results []map[string]interface{}

	// Используем JOIN и агрегацию для получения контрагентов с балансами
	query := `
		SELECT 
			c.id,
			c.name,
			c.inn,
			c.type,
			c.status,
			COALESCE(SUM(CASE WHEN ft.type = 'expense' THEN ft.amount ELSE 0 END), 0) as total_expenses,
			COALESCE(SUM(CASE WHEN ft.type = 'income' THEN ft.amount ELSE 0 END), 0) as total_income,
			COALESCE(SUM(CASE WHEN ft.type = 'expense' AND ft.status = 'Pending' THEN ft.amount ELSE 0 END), 0) as pending_expenses,
			COUNT(DISTINCT ft.id) as transaction_count
		FROM counterparties c
		LEFT JOIN finance_transactions ft ON c.id = ft.counterparty_id AND ft.deleted_at IS NULL
		WHERE c.status = 'Active' AND c.deleted_at IS NULL
		GROUP BY c.id, c.name, c.inn, c.type, c.status
		ORDER BY c.name
	`

	rows, err := s.db.Raw(query).Rows()
	if err != nil {
		return nil, fmt.Errorf("ошибка выполнения запроса: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var result map[string]interface{} = make(map[string]interface{})
		var id, name, inn, ctype, status string
		var totalExpenses, totalIncome, pendingExpenses float64
		var transactionCount int64

		if err := rows.Scan(&id, &name, &inn, &ctype, &status, &totalExpenses, &totalIncome, &pendingExpenses, &transactionCount); err != nil {
			log.Printf("⚠️ Ошибка сканирования строки: %v", err)
			continue
		}

		result["id"] = id
		result["name"] = name
		result["inn"] = inn
		result["type"] = ctype
		result["status"] = status
		result["total_expenses"] = totalExpenses
		result["total_income"] = totalIncome
		result["pending_expenses"] = pendingExpenses
		result["transaction_count"] = transactionCount
		result["net_balance"] = totalIncome - totalExpenses // Чистый баланс (доходы - расходы)

		results = append(results, result)
	}

	return results, nil
}

