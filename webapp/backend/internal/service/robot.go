package service

import (
	"backend/internal/model"
	"backend/internal/repository"
	"backend/internal/service/utils"
	"context"
	"math/bits"
	"log"
)

type RobotService struct {
	store *repository.Store
}

func NewRobotService(store *repository.Store) *RobotService {
	return &RobotService{store: store}
}

func (s *RobotService) GenerateDeliveryPlan(ctx context.Context, robotID string, capacity int) (*model.DeliveryPlan, error) {
	var plan model.DeliveryPlan

	err := utils.WithTimeout(ctx, func(ctx context.Context) error {
		return s.store.ExecTx(ctx, func(txStore *repository.Store) error {
			orders, err := txStore.OrderRepo.GetShippingOrders(ctx)
			if err != nil {
				return err
			}
			plan, err = selectOrdersForDelivery(ctx, orders, robotID, capacity)
			if err != nil {
				return err
			}
			if len(plan.Orders) > 0 {
				orderIDs := make([]int64, len(plan.Orders))
				for i, order := range plan.Orders {
					orderIDs[i] = order.OrderID
				}

				if err := txStore.OrderRepo.UpdateStatuses(ctx, orderIDs, "delivering"); err != nil {
					return err
				}
				log.Printf("Updated status to 'delivering' for %d orders", len(orderIDs))
			}
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return &plan, nil
}

func (s *RobotService) UpdateOrderStatus(ctx context.Context, orderID int64, newStatus string) error {
	return utils.WithTimeout(ctx, func(ctx context.Context) error {
		return s.store.OrderRepo.UpdateStatuses(ctx, []int64{orderID}, newStatus)
	})
}

func selectOrdersForDelivery(ctx context.Context, orders []model.Order, robotID string, capacity int) (model.DeliveryPlan, error) {
	n, W := len(orders), capacity
	if n == 0 || W <= 0 {
		return model.DeliveryPlan{
            RobotID: robotID,
            Orders:  []model.Order{}, // ★ nil ではなく空スライス
        }, nil
	}

	// 早期：全部載る
	sumW, sumV := 0, 0
	for _, o := range orders {
		sumW += o.Weight
		sumV += o.Value
	}
	if sumW <= W {
		best := make([]model.Order, n)
		copy(best, orders)
		return model.DeliveryPlan{
			RobotID:     robotID,
			TotalWeight: sumW,
			TotalValue:  sumV,
			Orders:      best,
		}, nil
	}

	// === GCD圧縮（入力は破壊しない） ===
	g := 0
	wts := make([]int, n)
	vals := make([]int, n)
	for i, o := range orders {
		wts[i], vals[i] = o.Weight, o.Value
		if o.Weight > 0 {
			g = gcd(g, o.Weight)
		}
	}
	if g > 1 {
		for i := range wts {
			if wts[i] > 0 {
				wts[i] /= g
			}
		}
		W /= g // gで割った容量（floor）。可行集合は不変。
	}
	if W < 0 {
		W = 0
	}

	// === 2行DP（prev/cur） + chooseビットセット（復元用） ===
	dpPrev := make([]int, W+1)
	dpCur := make([]int, W+1)

	// choose[i] は「i番目（1..n）のアイテムを取ったとき true」の重さw集合（ビットセット）
	words := (W>>6 + 1)
	choose := make([][]uint64, n+1) // 0行目は未使用
	for i := 0; i <= n; i++ {
		choose[i] = make([]uint64, words)
	}

	// 到達性（prev行の到達 w を保持）。reachPrev[0] の bit0 = w=0
	reachPrev := make([]uint64, words)
	reachPrev[0] = 1
	reachHiPrev := 0

	const checkEvery = 8192
	steps := 0

	maskUpTo := func(max int) uint64 {
		m := uint(max & 63)
		if m == 63 {
			return ^uint64(0)
		}
		return (uint64(1) << (m + 1)) - 1
	}

	for i := 1; i <= n; i++ {
		wt, val := wts[i-1], vals[i-1]
		// デフォルトは「選ばない」→ dpCur = dpPrev
		copy(dpCur, dpPrev)

		// 今回の到達性（次の行）
		reachCur := make([]uint64, words)
		copy(reachCur, reachPrev)
		reachHiCur := reachHiPrev

		if wt > 0 && wt <= W && val >= 0 {
			// base を列挙：w = base + wt が W 以内
			baseMax := reachHiPrev
			if baseMax > W-wt {
				baseMax = W - wt
			}
			if baseMax >= 0 {
				lastWord := baseMax >> 6
				for wi := lastWord; wi >= 0; wi-- {
					word := reachPrev[wi]
					if wi == lastWord {
						word &= maskUpTo(baseMax)
					}
					for word != 0 {
						// ワード内の最上位 set bit
						b := bits.Len64(word) - 1
						base := (wi << 6) + b
						w := base + wt

						steps++
						if steps%checkEvery == 0 {
							select {
							case <-ctx.Done():
								return model.DeliveryPlan{}, ctx.Err()
							default:
							}
						}

						// 同値は更新しない（= スキップ優先、DFSと一致）
						if nv := dpPrev[base] + val; nv > dpCur[w] {
							dpCur[w] = nv
							// choose[i][w] を true に
							choose[i][w>>6] |= (uint64(1) << uint(w&63))
							// 到達性更新
							wp, bp := w>>6, uint(w&63)
							if (reachCur[wp]>>bp)&1 == 0 {
								reachCur[wp] |= (uint64(1) << bp)
								if w > reachHiCur {
									reachHiCur = w
								}
							}
						}
						// 最上位bitを落とす
						word &^= (uint64(1) << uint(b))
					}
				}
			}
		}

		// 行入替
		dpPrev, dpCur = dpCur, dpPrev
		reachPrev, reachHiPrev = reachCur, reachHiCur
	}

	// 最終価値と、最大価値を達成する「最大の重さ」bestW（持ち上げ対策）
	bestValue := dpPrev[W]
	bestW := W
	for w := W; w >= 0; w-- {
		if dpPrev[w] == bestValue {
			bestW = w
			break
		}
	}

	// 復元：choose を i=n..1 で逆に辿る（wは圧縮容量）
	w := bestW
	selected := make([]bool, n)
	for i := n; i >= 1 && w >= 0; i-- {
		row := choose[i]
		if ((row[w>>6] >> uint(w&63)) & 1) == 1 {
			selected[i-1] = true
			w -= wts[i-1]
		}
	}

	// 返却（入力順）。重さは元単位に戻す。
	bestSet := make([]model.Order, 0)
	totalWeight := 0
	for i := 0; i < n; i++ {
		if selected[i] {
			bestSet = append(bestSet, orders[i])
			totalWeight += orders[i].Weight
		}
	}

	// 最後にキャンセルチェック
	select {
	case <-ctx.Done():
		return model.DeliveryPlan{}, ctx.Err()
	default:
	}

	return model.DeliveryPlan{
		RobotID:     robotID,
		TotalWeight: totalWeight,  // 例: 50
		TotalValue:  bestValue,    // 例: 236
		Orders:      bestSet,      // 期待のorder集合
	}, nil
}

func gcd(a, b int) int {
	if a == 0 {
		return b
	}
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}