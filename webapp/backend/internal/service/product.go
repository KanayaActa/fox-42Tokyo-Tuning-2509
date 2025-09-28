package service

import (
	"context"
	"log"

	"backend/internal/model"
	"backend/internal/repository"
)

type ProductService struct {
	store *repository.Store
}

func NewProductService(store *repository.Store) *ProductService {
	return &ProductService{store: store}
}

func (s *ProductService) CreateOrders(ctx context.Context, userID int, items []model.RequestItem) ([]string, error) {
	var insertedOrderIDs []string

	err := s.store.ExecTx(ctx, func(txStore *repository.Store) error {
		itemsToProcess := make(map[int]int)
		for _, item := range items {
			if item.Quantity > 0 {
				itemsToProcess[item.ProductID] = item.Quantity
			}
		}
		if len(itemsToProcess) == 0 {
			return nil
		}

		// 全ての注文をスライスに格納
		var orders []*model.Order
		for pID, quantity := range itemsToProcess {
			for i := 0; i < quantity; i++ {
				order := &model.Order{
					UserID:    userID,
					ProductID: pID,
				}
				orders = append(orders, order)
			}
		}

		// バッチINSERTで一括作成
		orderIDs, err := txStore.OrderRepo.CreateBatch(ctx, orders)
		if err != nil {
			return err
		}
		insertedOrderIDs = orderIDs
		
		return nil
	})

	if err != nil {
		return nil, err
	}
	log.Printf("Created %d orders for user %d", len(insertedOrderIDs), userID)
	return insertedOrderIDs, nil
}

func (s *ProductService) FetchProducts(ctx context.Context, userID int, req model.ListRequest) ([]model.Product, int, error) {
	products, total, err := s.store.ProductRepo.ListProducts(ctx, userID, req)
	return products, total, err
}
