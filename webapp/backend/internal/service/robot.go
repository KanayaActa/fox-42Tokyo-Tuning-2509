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

func selectOrdersForDelivery(ctx context.Context, orders []model.Order, robotID string, robotCapacity int) (model.DeliveryPlan, error) {
	n, W := len(orders), robotCapacity
	if n == 0 || W <= 0 {
		return model.DeliveryPlan{RobotID: robotID}, nil
	}

	// 早期リターン：全部載るならそのまま返す
	sumW, sumV := 0, 0
	for _, o := range orders {
		sumW += o.Weight
		sumV += o.Value
	}
	if sumW <= W {
		best := make([]model.Order, 0, n)
		best = append(best, orders...)
		return model.DeliveryPlan{
			RobotID:     robotID,
			TotalWeight: sumW,
			TotalValue:  sumV,
			Orders:      best,
		}, nil
	}

	// GCD圧縮（重さ単位を圧縮：解は不変でループが速くなる）
	g := 0
	for _, o := range orders {
		if o.Weight > 0 {
			g = gcd(g, o.Weight)
		}
	}
	if g > 1 {
		for i := range orders {
			if orders[i].Weight > 0 {
				orders[i].Weight /= g
			}
		}
		W /= g
	}

	// 1D DP（価値最大）
	dp := make([]int, W+1)
	const none = -1
	pick := make([]int, W+1) // dp[w] を最後に更新した item index
	prev := make([]int, W+1) // そのときの遷移元容量
	for w := 0; w <= W; w++ {
		pick[w], prev[w] = none, none
	}

	// 到達性 bitset（w 到達済みか）: 64bit words
	words := (W >> 6) + 1
	reach := make([]uint64, words)
	reach[0] = 1 // w=0 到達

	reachHi := 0     // これまでの到達最大容量
	poll := 8192     // ctx ポーリング間引き
	maskUpTo := func(max int) uint64 {
		// 下位 (max%64 + 1)bit を 1 にしたマスク
		m := uint(max & 63)
		if m == 63 {
			return ^uint64(0)
		}
		return (uint64(1) << (m + 1)) - 1
	}

	for i := 0; i < n; i++ {
		itW, itV := orders[i].Weight, orders[i].Value
		if itW <= 0 || itV < 0 || itW > W {
			continue
		}

		// 今回の上限 w：これまでの到達上限 + itW （W を超えない）
		upper := reachHi + itW
		if upper > W {
			upper = W
		}
		if upper < itW {
			continue
		}

		// 到達スナップショット（このアイテムの“前”だけを使う＝多重選択防止）
		snap := make([]uint64, words)
		copy(snap, reach)

		// base = w - itW の最大値（= upper - itW）までの到達のみ列挙
		baseMax := upper - itW
		lastWord := baseMax >> 6

		// 降順で set bit を列挙（w を降順に更新）
		for wi := lastWord; wi >= 0; wi-- {
			word := snap[wi]
			if wi == lastWord {
				// 最後のワードは baseMax までにマスク
				word &= maskUpTo(baseMax)
			}
			for word != 0 {
				// ワード内の最上位 set bit を取り出す
				b := bits.Len64(word) - 1 // 0..63
				base := (wi << 6) + b
				w := base + itW

				// ctx.Done() の間引きチェック
				poll--
				if poll == 0 {
					poll = 8192
					select {
					case <-ctx.Done():
						return model.DeliveryPlan{}, ctx.Err()
					default:
					}
				}

				// 通常の 0/1 ナップサック更新（同値は更新しない＝skip-first）
				nv := dp[base] + itV
				if nv > dp[w] {
					dp[w] = nv
					pick[w] = i
					prev[w] = base
					// 到達性の更新
					wp := w >> 6
					bp := uint(w & 63)
					if (reach[wp]>>bp)&1 == 0 {
						reach[wp] |= (uint64(1) << bp)
						if w > reachHi {
							reachHi = w
						}
					}
				}
				// 最上位ビットを落とす
				word &^= (uint64(1) << uint(b))
			}
		}
	}

	// 最大価値は dp[W]
	bestValue := dp[W]
	// その最大価値を達成する「最大の重さ」から復元開始（持ち上げ対策）
	bestW := W
	for w := W; w >= 0; w-- {
		if dp[w] == bestValue {
			bestW = w
			break
		}
	}

	// pick[w] がない“持ち上げ区間”は w-- で飛ばし、更新点でのみ遡る
	selected := make([]bool, n)
	w := bestW
	for w > 0 {
		if pick[w] == none {
			w--
			continue
		}
		i := pick[w]
		if selected[i] {
			break // 念のため
		}
		selected[i] = true
		w = prev[w]
	}

	// 返却は入力順（DFSの順序性に合わせる）
	bestSet := make([]model.Order, 0)
	totalWeight := 0
	for i := 0; i < n; i++ {
		if selected[i] {
			bestSet = append(bestSet, orders[i])
			totalWeight += orders[i].Weight
		}
	}

	// GCD圧縮の重さを元に戻す（外部仕様が元単位なら）
	if g > 1 {
		totalWeight *= g
		for i := range bestSet {
			bestSet[i].Weight *= g
		}
	}

	// 最後のキャンセル確認
	select {
	case <-ctx.Done():
		return model.DeliveryPlan{}, ctx.Err()
	default:
	}

	return model.DeliveryPlan{
		RobotID:     robotID,
		TotalWeight: totalWeight,
		TotalValue:  bestValue,
		Orders:      bestSet,
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