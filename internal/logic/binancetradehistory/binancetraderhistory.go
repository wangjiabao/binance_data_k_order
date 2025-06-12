package logic

import (
	"binance_data_gf/internal/model/do"
	"binance_data_gf/internal/model/entity"
	"binance_data_gf/internal/service"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gateio/gateapi-go/v6"
	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/container/gqueue"
	"github.com/gogf/gf/v2/container/gset"
	"github.com/gogf/gf/v2/container/gtype"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/grpool"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/shopspring/decimal"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type (
	sBinanceTraderHistory struct {
		// 全局存储
		pool *grpool.Pool
		// 针对binance拉取交易员下单接口定制的数据结构，每页数据即使同一个ip不同的交易员是可以返回数据，在逻辑上和这个设计方式要达成共识再设计后续的程序逻辑。
		// 考虑实际的业务场景，接口最多6000条，每次查50条，最多需要120个槽位。假设，每10个槽位一循环，意味着对应同一个交易员每次并行使用10个ip查询10页数据。
		//ips map[int]*proxyData
		ips        *gmap.IntStrMap
		orderQueue *gqueue.Queue
	}
)

func init() {
	service.RegisterBinanceTraderHistory(New())
}

func New() *sBinanceTraderHistory {
	return &sBinanceTraderHistory{
		grpool.New(), // 这里是请求协程池子，可以配合着可并行请求binance的限制使用，来限制最大共存数，后续jobs都将排队，考虑到上层的定时任务
		gmap.NewIntStrMap(true),
		gqueue.New(), // 下单顺序队列
	}
}

func IsEqual(f1, f2 float64) bool {
	if f1 > f2 {
		return f1-f2 < 0.000000001
	} else {
		return f2-f1 < 0.000000001
	}
}

func lessThanOrEqualZero(a, b float64, epsilon float64) bool {
	return a-b < epsilon || math.Abs(a-b) < epsilon
}

type proxyData struct {
	Ip   string
	Port int64
}

type proxyRep struct {
	Data []*proxyData
}

// 拉取代理列表，暂时弃用
func requestProxy() ([]*proxyData, error) {
	var (
		resp   *http.Response
		b      []byte
		res    []*proxyData
		err    error
		apiUrl = ""
	)

	httpClient := &http.Client{
		Timeout: time.Second * 10,
	}

	// 构造请求
	resp, err = httpClient.Get(apiUrl)
	if err != nil {
		return nil, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	var l *proxyRep
	err = json.Unmarshal(b, &l)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	res = make([]*proxyData, 0)
	if nil == l.Data || 0 >= len(l.Data) {
		return res, nil
	}

	for _, v := range l.Data {
		res = append(res, v)
	}

	return res, nil
}

// UpdateProxyIp ip更新
func (s *sBinanceTraderHistory) UpdateProxyIp(ctx context.Context) (err error) {
	// 20个客户端代理，这里注意一定是key是从0开始到size-1的
	s.ips.Set(0, "http://43.133.175.121:888/")
	s.ips.Set(1, "http://43.133.209.89:888/")
	s.ips.Set(2, "http://43.133.177.73:888/")
	s.ips.Set(3, "http://43.163.224.155:888/")
	s.ips.Set(4, "http://43.163.210.110:888/")
	s.ips.Set(5, "http://43.163.242.180:888/")
	s.ips.Set(6, "http://43.153.162.213:888/")
	s.ips.Set(7, "http://43.163.197.191:888/")
	s.ips.Set(8, "http://43.163.217.70:888/")
	s.ips.Set(9, "http://43.163.194.135:888/")
	s.ips.Set(10, "http://43.130.254.24:888/")
	s.ips.Set(11, "http://43.128.249.92:888/")
	s.ips.Set(12, "http://43.153.177.107:888/")
	s.ips.Set(13, "http://43.133.15.135:888/")
	s.ips.Set(14, "http://43.130.229.66:888/")
	s.ips.Set(15, "http://43.133.196.220:888/")
	s.ips.Set(16, "http://43.163.208.36:888/")
	s.ips.Set(17, "http://43.133.204.254:888/")
	s.ips.Set(18, "http://43.153.181.89:888/")
	s.ips.Set(19, "http://43.163.231.217:888/")

	return nil

	//res := make([]*proxyData, 0)
	//for i := 0; i < 1000; i++ {
	//	var (
	//		resTmp []*proxyData
	//	)
	//	resTmp, err = requestProxy()
	//	if nil != err {
	//		fmt.Println("ip池子更新出错", err)
	//		time.Sleep(time.Second * 1)
	//		continue
	//	}
	//
	//	if 0 < len(resTmp) {
	//		res = append(res, resTmp...)
	//	}
	//
	//	if 20 <= len(res) { // 20个最小
	//		break
	//	}
	//
	//	fmt.Println("ip池子更新时无数据")
	//	time.Sleep(time.Second * 1)
	//}
	//
	//s.ips.Clear()
	//
	//// 更新
	//for k, v := range res {
	//	s.ips.Set(k, "http://"+v.Ip+":"+strconv.FormatInt(v.Port, 10)+"/")
	//}
	//
	//fmt.Println("ip池子更新成功", time.Now(), s.ips.Size())
	//return nil
}

// PullAndOrder 拉取binance数据
func (s *sBinanceTraderHistory) PullAndOrder(ctx context.Context, traderNum uint64) (err error) {
	start := time.Now()

	//// 测试部分注释
	//_, err = s.pullAndSetHandle(ctx, traderNum, 120) // 执行
	//fmt.Println("ok", traderNum)
	//return nil

	// 测试部分注释
	//if 1 == traderNum {
	//	fmt.Println("此时系统，workers：", pool.Size(), "jobs：", pool.Jobs())
	//	return nil
	//}

	/**
	 * 这里说明一下同步规则
	 * 首先任务已经是每个交易员的单独协程了
	 *
	 * 步骤1：
	 * 探测任务：拉取第1页10条，前10条与数据库中前10条对比，一摸一样认为无新订单（继续进行探测），否则有新订单进入步骤2。
	 * 步骤2：
	 * 下单任务：并行5个任务，每个任务表示每页数据的拉取，每次5页共250条，拉取后重新拉取第1页数据与刚才的5页中的第1页，
	 * 对比10条，一模一样表示这段任务执行期间无更新，否则全部放弃，重新开始步骤2。
	 *
	 * ip池子的加入
	 * 步骤1中探测任务可以考虑分配一个ip。
	 * 步骤2中每个任务分配不同的ip（防止ip封禁用，目前经验是binance对每个ip在查询每个交易员数据时有2秒的限制，并行则需要不同的ip）
	 *
	 * 数据库不存在数据时，直接执行步骤2，并行5页，如果执行完任务，发现有新订单，则全部放弃，重新步骤2。
	 */

	// 数据库对比数据
	var (
		compareMax                     = 10 // 预设最大对比条数，小于最大限制10条，注意：不能超过50条，在程序多出有写死，binance目前每页最大条数
		currentCompareMax              int  // 实际获得对比条数
		binanceTradeHistoryNewestGroup []*entity.NewBinanceTradeHistory
		resData                        []*entity.NewBinanceTradeHistory
		resDataCompare                 []*entity.NewBinanceTradeHistory
		initPull                       bool
	)

	err = g.Model("new_binance_trade_" + strconv.FormatUint(traderNum, 10) + "_history").Ctx(ctx).Limit(compareMax).OrderDesc("id").Scan(&binanceTradeHistoryNewestGroup)
	if nil != err {
		return err
	}

	currentCompareMax = len(binanceTradeHistoryNewestGroup)

	ipMapNeedWait := make(map[string]bool, 0) // 刚使用的ip，大概率加快查询速度，2s以内别用的ip
	// 数据库无数据，拉取满额6000条数据
	if 0 >= currentCompareMax {
		initPull = true
		resData, err = s.pullAndSetHandle(ctx, traderNum, 120, true, ipMapNeedWait) // 执行
		if nil != err {
			fmt.Println("初始化，执行拉取数据协程异常，错误信息：", err, "交易员：", traderNum)
		}

		if nil == resData {
			fmt.Println("初始化，执行拉取数据协程异常，数据缺失", "交易员：", traderNum)
			return nil
		}

		if 0 >= len(resData) {
			fmt.Println("初始化，执行拉取数据协程异常，空数据：", len(resData), "交易员：", traderNum)
			return nil
		}

		var (
			compareResDiff bool
		)
		// 截取前10条记录
		afterCompare := make([]*entity.NewBinanceTradeHistory, 0)
		if len(resData) >= compareMax {
			afterCompare = resData[:compareMax]
		} else {
			// 这里也限制了最小入库的交易员数据条数
			fmt.Println("初始化，执行拉取数据协程异常，条数不足，条数：", len(resData), "交易员：", traderNum)
			return nil
		}

		if 0 >= len(afterCompare) {
			fmt.Println("初始化，执行拉取数据协程异常，条数不足，条数：", len(resData), "交易员：", traderNum)
			return nil
		}

		// todo 如果未来初始化仓位不准，那这里应该很多的嫌疑，因为在拉取中第一页数据的协程最后执行完，那么即使带单员更新了，也不会被察觉，当然概率很小
		// 有问题的话，可以换成重新执行一次完整的拉取，比较
		_, _, compareResDiff, err = s.compareBinanceTradeHistoryPageOne(int64(compareMax), traderNum, afterCompare)
		if nil != err {
			return err
		}

		// 不同，则拉取数据的时间有新单，放弃操作，等待下次执行
		if compareResDiff {
			fmt.Println("初始化，执行拉取数据协程异常，有新单", "交易员：", traderNum)
			return nil
		}

	} else if compareMax <= currentCompareMax {
		/**
		 * 试探开始
		 * todo
		 * 假设：binance的数据同一时间的数据乱序出现时，因为和数据库前n条不一致，而认为是新数据，后续保存会有处理，但是这里就会一直生效，现在这个假设还未出现。*
		 * 因为保存对上述假设的限制，延迟出现的同一时刻的数据一直不会被系统保存，而每次都会触发这里的比较，得到不同数据，为了防止乱序的假设最后是这样做，但是可能导致一直拉取10页流量增长，后续观察假设不存在最好，假设存在更新方案。
		 */
		var (
			newData        []*binanceTradeHistoryDataList
			compareResDiff bool
		)

		ipMapNeedWait, newData, compareResDiff, err = s.compareBinanceTradeHistoryPageOne(int64(compareMax), traderNum, binanceTradeHistoryNewestGroup)
		if nil != err {
			return err
		}

		// 相同，返回
		if !compareResDiff {
			return nil
		}

		if nil == newData || 0 >= len(newData) {
			fmt.Println("日常，执行拉取数据协程异常，新数据未空，错误信息：", err, "交易员：", traderNum)
			return nil
		}

		fmt.Println("新数据：交易员：", traderNum)

		// 不同，开始捕获
		resData, err = s.pullAndSetHandle(ctx, traderNum, 10, true, ipMapNeedWait) // todo 执行，目前猜测最大500条，根据经验拍脑袋
		if nil != err {
			fmt.Println("日常，执行拉取数据协程异常，错误信息：", err, "交易员：", traderNum)
		}

		if nil == resData {
			fmt.Println("日常，执行拉取数据协程异常，数据缺失", "交易员：", traderNum)
			return nil
		}

		if 0 >= len(resData) {
			fmt.Println("日常，执行拉取数据协程异常，数据为空", "交易员：", traderNum)
			return nil
		}

		// 重新拉取，比较探测的结果，和最后的锁定结果
		resDataCompare, err = s.pullAndSetHandle(ctx, traderNum, 1, false, ipMapNeedWait) // todo 执行，目前猜测最大500条，根据经验拍脑袋
		if nil != err {
			fmt.Println("日常，执行拉取数据协程异常，比较数据，错误信息：", err, "交易员：", traderNum)
		}

		if nil == resDataCompare {
			fmt.Println("日常，执行拉取数据协程异常，比较数据，数据缺失", "交易员：", traderNum)
			return nil
		}

		for kNewData, vNewData := range newData {
			// 比较
			if !(vNewData.Time == resDataCompare[kNewData].Time &&
				vNewData.Symbol == resDataCompare[kNewData].Symbol &&
				vNewData.Side == resDataCompare[kNewData].Side &&
				vNewData.PositionSide == resDataCompare[kNewData].PositionSide &&
				IsEqual(vNewData.Qty, resDataCompare[kNewData].Qty) && // 数量
				IsEqual(vNewData.Price, resDataCompare[kNewData].Price) && //价格
				IsEqual(vNewData.RealizedProfit, resDataCompare[kNewData].RealizedProfit) &&
				IsEqual(vNewData.Quantity, resDataCompare[kNewData].Quantity) &&
				IsEqual(vNewData.Fee, resDataCompare[kNewData].Fee)) {
				fmt.Println("日常，执行拉取数据协程异常，比较数据，数据不同，第一条", "交易员：", traderNum, resData[0], resDataCompare[0])
				return nil
			}
		}

	} else {
		fmt.Println("执行拉取数据协程异常，查询数据库数据条数不在范围：", compareMax, currentCompareMax, "交易员：", traderNum, "初始化：", initPull)
	}

	// 时长
	fmt.Printf("程序拉取部分，开始 %v, 时长: %v, 交易员: %v\n", start, time.Since(start), traderNum)

	// 非初始化，截断数据
	if !initPull {
		tmpResData := make([]*entity.NewBinanceTradeHistory, 0)
		//tmpCurrentCompareMax := currentCompareMax
		for _, vResData := range resData {
			/**
			 * todo
			 * 停下的条件，暂时是：
			 * 1 数据库的最新一条比较，遇到时间，币种，方向一致的订单，认为是已经纳入数据库的。
			 * 2 如果数据库最新的时间已经晚于遍历时遇到的时间也一定停下，这里会有一个好处，即使binance的数据同一时间的数据总是乱序出现时，我们也不会因为和数据库第一条不一致，而认为是新数据。
			 *
			 * 如果存在误判的原因，
			 * 1 情况是在上次执行完拉取保存后，相同时间的数据，因为binance的问题，延迟出现了。
			 */
			if vResData.Time == binanceTradeHistoryNewestGroup[0].Time &&
				vResData.Side == binanceTradeHistoryNewestGroup[0].Side &&
				vResData.PositionSide == binanceTradeHistoryNewestGroup[0].PositionSide &&
				vResData.Symbol == binanceTradeHistoryNewestGroup[0].Symbol {
				break
			}

			if vResData.Time <= binanceTradeHistoryNewestGroup[0].Time {
				fmt.Println("遍历时竟然未发现相同数据！此时数据时间已经小于了数据库最新一条的时间，如果时间相同可能是binance延迟出现的数据，数据：", vResData, binanceTradeHistoryNewestGroup[0])
				break
			}

			//if (len(resData) - k) <= tmpCurrentCompareMax { // 还剩下几条
			//	tmpCurrentCompareMax = len(resData) - k
			//}

			//tmp := 0
			//if 0 < tmpCurrentCompareMax {
			//	for i := 0; i < tmpCurrentCompareMax; i++ { // todo 如果只剩下最大条数以内的数字，只能兼容着比较，这里根据经验判断会不会出现吧
			//		if resData[k+i].Time == binanceTradeHistoryNewestGroup[i].Time &&
			//			resData[k+i].Symbol == binanceTradeHistoryNewestGroup[i].Symbol &&
			//			resData[k+i].Side == binanceTradeHistoryNewestGroup[i].Side &&
			//			resData[k+i].PositionSide == binanceTradeHistoryNewestGroup[i].PositionSide &&
			//			IsEqual(resData[k+i].Qty, binanceTradeHistoryNewestGroup[i].Qty) && // 数量
			//			IsEqual(resData[k+i].Price, binanceTradeHistoryNewestGroup[i].Price) && //价格
			//			IsEqual(resData[k+i].RealizedProfit, binanceTradeHistoryNewestGroup[i].RealizedProfit) &&
			//			IsEqual(resData[k+i].Quantity, binanceTradeHistoryNewestGroup[i].Quantity) &&
			//			IsEqual(resData[k+i].Fee, binanceTradeHistoryNewestGroup[i].Fee) {
			//			tmp++
			//		}
			//	}
			//
			//	if tmpCurrentCompareMax == tmp {
			//		break
			//	}
			//} else {
			//	break
			//}

			tmpResData = append(tmpResData, vResData)
		}

		resData = tmpResData
	}

	insertData := make([]*do.NewBinanceTradeHistory, 0)
	// 数据倒序插入，程序走到这里，最多会拉下来初始化：6000，日常：500，最少10条（前边的条件限制）
	for i := len(resData) - 1; i >= 0; i-- {
		insertData = append(insertData, &do.NewBinanceTradeHistory{
			Time:                resData[i].Time,
			Symbol:              resData[i].Symbol,
			Side:                resData[i].Side,
			PositionSide:        resData[i].PositionSide,
			Price:               resData[i].Price,
			Fee:                 resData[i].Fee,
			FeeAsset:            resData[i].FeeAsset,
			Quantity:            resData[i].Quantity,
			QuantityAsset:       resData[i].QuantityAsset,
			RealizedProfit:      resData[i].RealizedProfit,
			RealizedProfitAsset: resData[i].RealizedProfitAsset,
			BaseAsset:           resData[i].BaseAsset,
			Qty:                 resData[i].Qty,
			ActiveBuy:           resData[i].ActiveBuy,
		})
	}

	// 入库
	if 0 >= len(insertData) {
		return nil
	}

	// 推入下单队列
	// todo 这种队列的方式可能存在生产者或消费者出现问题，而丢单的情况，可以考虑更换复杂的方式，即使丢单开不起来会影响开单，关单的话少关单，有仓位检测的二重保障
	pushDataMap := make(map[string]*binanceTrade, 0)
	pushData := make([]*binanceTrade, 0)

	// 代币
	for _, vInsertData := range insertData {
		// 代币，仓位，方向，同一秒 暂时看作一次下单
		timeTmp := vInsertData.Time.(uint64)
		if _, ok := pushDataMap[vInsertData.Symbol.(string)+vInsertData.PositionSide.(string)+vInsertData.Side.(string)+strconv.FormatUint(timeTmp, 10)]; !ok {
			pushDataMap[vInsertData.Symbol.(string)+vInsertData.PositionSide.(string)+vInsertData.Side.(string)+strconv.FormatUint(timeTmp, 10)] = &binanceTrade{
				TraderNum: traderNum,
				Type:      vInsertData.PositionSide.(string),
				Symbol:    vInsertData.Symbol.(string),
				Side:      vInsertData.Side.(string),
				Position:  "",
				Qty:       "",
				QtyFloat:  vInsertData.Qty.(float64),
				Time:      timeTmp,
			}
		} else { // 到这里一定存在了，累加
			pushDataMap[vInsertData.Symbol.(string)+vInsertData.PositionSide.(string)+vInsertData.Side.(string)+strconv.FormatUint(timeTmp, 10)].QtyFloat += vInsertData.Qty.(float64)
		}
	}

	if 0 < len(pushDataMap) {
		for _, vPushDataMap := range pushDataMap {
			pushData = append(pushData, vPushDataMap)
		}

		if 0 < len(pushData) {
			// 排序，时间靠前的在前边处理
			sort.Slice(pushData, func(i, j int) bool {
				return pushData[i].Time < pushData[j].Time
			})
		}
	}

	err = g.DB().Transaction(context.TODO(), func(ctx context.Context, tx gdb.TX) error {
		// 日常更新数据
		if 0 < len(pushData) {
			// 先查更新仓位，代币，仓位，方向归集好
			for _, vPushDataMap := range pushData {
				// 查询最新未关仓仓位
				var (
					selectOne []*entity.NewBinancePositionHistory
				)
				err = tx.Ctx(ctx).Model("new_binance_position_"+strconv.FormatUint(traderNum, 10)+"_history").
					Where("symbol=?", vPushDataMap.Symbol).Where("side=?", vPushDataMap.Type).Where("opened<=?", vPushDataMap.Time).Where("closed=?", 0).Where("qty>?", 0).
					OrderDesc("id").Limit(1).Scan(&selectOne)
				if err != nil {
					return err
				}

				if 0 >= len(selectOne) {
					// 新增仓位
					if ("LONG" == vPushDataMap.Type && "BUY" == vPushDataMap.Side) ||
						("SHORT" == vPushDataMap.Type && "SELL" == vPushDataMap.Side) {
						// 开仓
						_, err = tx.Ctx(ctx).Insert("new_binance_position_"+strconv.FormatUint(traderNum, 10)+"_history", &do.NewBinancePositionHistory{
							Closed: 0,
							Opened: vPushDataMap.Time,
							Symbol: vPushDataMap.Symbol,
							Side:   vPushDataMap.Type,
							Status: "",
							Qty:    vPushDataMap.QtyFloat,
						})
						if err != nil {
							return err
						}
					}
				} else {
					// 修改仓位
					if ("LONG" == vPushDataMap.Type && "SELL" == vPushDataMap.Side) ||
						("SHORT" == vPushDataMap.Type && "BUY" == vPushDataMap.Side) {
						// 平空 || 平多
						var (
							updateData g.Map
						)

						// todo bsc最高精度小数点7位，到了6位的情况非常少，没意义，几乎等于完全平仓
						if lessThanOrEqualZero(selectOne[0].Qty, vPushDataMap.QtyFloat, 1e-6) {
							updateData = g.Map{
								"qty":    0,
								"closed": gtime.Now().UnixMilli(),
							}

							// 平仓前仓位
							vPushDataMap.Position = strconv.FormatFloat(vPushDataMap.QtyFloat, 'f', -1, 64)
						} else {
							updateData = g.Map{
								"qty": &gdb.Counter{
									Field: "qty",
									Value: -vPushDataMap.QtyFloat, // 加 -值
								},
							}

							// 平仓前仓位
							vPushDataMap.Position = strconv.FormatFloat(selectOne[0].Qty, 'f', -1, 64)
						}

						_, err = tx.Ctx(ctx).Update("new_binance_position_"+strconv.FormatUint(traderNum, 10)+"_history", updateData, "id", selectOne[0].Id)
						if nil != err {
							return err
						}

					} else if ("LONG" == vPushDataMap.Type && "BUY" == vPushDataMap.Side) ||
						("SHORT" == vPushDataMap.Type && "SELL" == vPushDataMap.Side) {
						// 开多 || 开空
						updateData := g.Map{
							"qty": &gdb.Counter{
								Field: "qty",
								Value: vPushDataMap.QtyFloat,
							},
						}

						_, err = tx.Ctx(ctx).Update("new_binance_position_"+strconv.FormatUint(traderNum, 10)+"_history", updateData, "id", selectOne[0].Id)
						if nil != err {
							return err
						}
					}
				}
			}
		}

		batchSize := 500
		for i := 0; i < len(insertData); i += batchSize {
			end := i + batchSize
			if end > len(insertData) {
				end = len(insertData)
			}
			batch := insertData[i:end]

			_, err = tx.Ctx(ctx).Insert("new_binance_trade_"+strconv.FormatUint(traderNum, 10)+"_history", batch)
			if err != nil {
				return err
			}
		}

		return nil
	})
	if nil != err {
		return err
	}

	// 推入下单队列
	// todo 这种队列的方式可能存在生产者或消费者出现问题，而丢单的情况，可以考虑更换复杂的方式，即使丢单开不起来会影响开单，关单的话少关单，有仓位检测的二重保障
	if !initPull && 0 < len(pushData) {
		s.orderQueue.Push(pushData)
	}

	fmt.Println("更新结束，交易员：", traderNum)
	return nil
}

// PullAndOrderNew 拉取binance数据，仓位，根据cookie
func (s *sBinanceTraderHistory) PullAndOrderNew(ctx context.Context, traderNum uint64, ipProxyUse int) (err error) {
	//start := time.Now()
	//var (
	//	trader                    []*entity.Trader
	//	zyTraderCookie            []*entity.ZyTraderCookie
	//	binancePosition           []*entity.TraderPosition
	//	binancePositionMap        map[string]*entity.TraderPosition
	//	binancePositionMapCompare map[string]*entity.TraderPosition
	//	reqResData                []*binancePositionDataList
	//)
	//
	//// 数据库必须信息
	//err = g.Model("trader_position_" + strconv.FormatUint(traderNum, 10)).Ctx(ctx).Scan(&binancePosition)
	//if nil != err {
	//	return err
	//}
	//binancePositionMap = make(map[string]*entity.TraderPosition, 0)
	//binancePositionMapCompare = make(map[string]*entity.TraderPosition, 0)
	//for _, vBinancePosition := range binancePosition {
	//	binancePositionMap[vBinancePosition.Symbol+vBinancePosition.PositionSide] = vBinancePosition
	//	binancePositionMapCompare[vBinancePosition.Symbol+vBinancePosition.PositionSide] = vBinancePosition
	//}
	//
	//// 数据库必须信息
	//err = g.Model("trader").Ctx(ctx).Where("portfolioId=?", traderNum).OrderDesc("id").Limit(1).Scan(&trader)
	//if nil != err {
	//	return err
	//}
	//if 0 >= len(trader) {
	//	//fmt.Println("新，不存在trader表：信息", traderNum, ipProxyUse)
	//	return nil
	//}
	//
	//// 数据库必须信息
	//err = g.Model("zy_trader_cookie").Ctx(ctx).Where("trader_id=? and is_open=?", trader[0].Id, 1).
	//	OrderDesc("update_time").Limit(1).Scan(&zyTraderCookie)
	//if nil != err {
	//	return err
	//}
	//if 0 >= len(zyTraderCookie) || 0 >= len(zyTraderCookie[0].Cookie) || 0 >= len(zyTraderCookie[0].Token) {
	//	return nil
	//}
	//
	//// 执行
	//var (
	//	retry           = false
	//	retryTimes      = 0
	//	retryTimesLimit = 5 // 重试次数
	//	cookieErr       = false
	//)
	//
	//for retryTimes < retryTimesLimit { // 最大重试
	//	//reqResData, retry, err = s.requestProxyBinancePositionHistoryNew(s.ips.Get(ipProxyUse%(s.ips.Size()-1)), traderNum)
	//	reqResData, retry, err = s.requestBinancePositionHistoryNew(traderNum, zyTraderCookie[0].Cookie, zyTraderCookie[0].Token)
	//
	//	// 需要重试
	//	if retry {
	//		retryTimes++
	//		continue
	//	}
	//
	//	// cookie不好使
	//	if 0 >= len(reqResData) {
	//		retryTimes++
	//		cookieErr = true
	//		continue
	//	} else {
	//		cookieErr = false
	//		break
	//	}
	//}
	//
	//if cookieErr {
	//	fmt.Println("新，cookie错误，信息", traderNum, reqResData)
	//	err = g.DB().Transaction(context.TODO(), func(ctx context.Context, tx gdb.TX) error {
	//		zyTraderCookie[0].IsOpen = 0
	//		_, err = tx.Ctx(ctx).Update("zy_trader_cookie", zyTraderCookie[0], "id", zyTraderCookie[0].Id)
	//		if nil != err {
	//			return err
	//		}
	//
	//		return nil
	//	})
	//	if nil != err {
	//		fmt.Println("新，更新数据库错误，信息", traderNum, err)
	//		return err
	//	}
	//
	//	return nil
	//}
	//
	//// 用于数据库更新
	//insertData := make([]*do.TraderPosition, 0)
	//updateData := make([]*do.TraderPosition, 0)
	//// 用于下单
	//orderInsertData := make([]*do.TraderPosition, 0)
	//orderUpdateData := make([]*do.TraderPosition, 0)
	//for _, vReqResData := range reqResData {
	//	// 新增
	//	var (
	//		currentAmount    float64
	//		currentAmountAbs float64
	//	)
	//	currentAmount, err = strconv.ParseFloat(vReqResData.PositionAmount, 64)
	//	if nil != err {
	//		fmt.Println("新，解析金额出错，信息", vReqResData, currentAmount, traderNum)
	//	}
	//	currentAmountAbs = math.Abs(currentAmount) // 绝对值
	//
	//	if _, ok := binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide]; !ok {
	//		if "BOTH" != vReqResData.PositionSide { // 单项持仓
	//			// 加入数据库
	//			insertData = append(insertData, &do.TraderPosition{
	//				Symbol:         vReqResData.Symbol,
	//				PositionSide:   vReqResData.PositionSide,
	//				PositionAmount: currentAmountAbs,
	//			})
	//
	//			// 下单
	//			if IsEqual(currentAmountAbs, 0) {
	//				continue
	//			}
	//
	//			orderInsertData = append(orderInsertData, &do.TraderPosition{
	//				Symbol:         vReqResData.Symbol,
	//				PositionSide:   vReqResData.PositionSide,
	//				PositionAmount: currentAmountAbs,
	//			})
	//		} else {
	//			// 加入数据库
	//			insertData = append(insertData, &do.TraderPosition{
	//				Symbol:         vReqResData.Symbol,
	//				PositionSide:   vReqResData.PositionSide,
	//				PositionAmount: currentAmount, // 正负数保持
	//			})
	//
	//			// 模拟为多空仓，下单，todo 组合式的判断应该时牢靠的
	//			var tmpPositionSide string
	//			if IsEqual(currentAmount, 0) {
	//				continue
	//			} else if math.Signbit(currentAmount) {
	//				// 模拟空
	//				tmpPositionSide = "SHORT"
	//				orderInsertData = append(orderInsertData, &do.TraderPosition{
	//					Symbol:         vReqResData.Symbol,
	//					PositionSide:   tmpPositionSide,
	//					PositionAmount: currentAmountAbs, // 变成绝对值
	//				})
	//			} else {
	//				// 模拟多
	//				tmpPositionSide = "LONG"
	//				orderInsertData = append(orderInsertData, &do.TraderPosition{
	//					Symbol:         vReqResData.Symbol,
	//					PositionSide:   tmpPositionSide,
	//					PositionAmount: currentAmountAbs, // 变成绝对值
	//				})
	//			}
	//		}
	//	} else {
	//		// 数量无变化
	//		if "BOTH" != vReqResData.PositionSide {
	//			if IsEqual(currentAmountAbs, binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
	//				continue
	//			}
	//
	//			updateData = append(updateData, &do.TraderPosition{
	//				Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
	//				Symbol:         vReqResData.Symbol,
	//				PositionSide:   vReqResData.PositionSide,
	//				PositionAmount: currentAmountAbs,
	//			})
	//
	//			orderUpdateData = append(orderUpdateData, &do.TraderPosition{
	//				Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
	//				Symbol:         vReqResData.Symbol,
	//				PositionSide:   vReqResData.PositionSide,
	//				PositionAmount: currentAmountAbs,
	//			})
	//		} else {
	//			if IsEqual(currentAmount, binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
	//				continue
	//			}
	//
	//			updateData = append(updateData, &do.TraderPosition{
	//				Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
	//				Symbol:         vReqResData.Symbol,
	//				PositionSide:   vReqResData.PositionSide,
	//				PositionAmount: currentAmount, // 正负数保持
	//			})
	//
	//			// 第一步：构造虚拟的上一次仓位，空或多或无
	//			// 这里修改一下历史仓位的信息，方便程序在后续的流程中使用，模拟both的positionAmount为正数时，修改仓位对应的多仓方向的数据，为负数时修改空仓位的数据，0时不处理
	//			if _, ok = binancePositionMap[vReqResData.Symbol+"SHORT"]; !ok {
	//				fmt.Println("新，缺少仓位SHORT，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
	//				continue
	//			}
	//			if _, ok = binancePositionMap[vReqResData.Symbol+"LONG"]; !ok {
	//				fmt.Println("新，缺少仓位LONG，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
	//				continue
	//			}
	//
	//			var lastPositionSide string // 上次仓位
	//			binancePositionMapCompare[vReqResData.Symbol+"SHORT"] = &entity.TraderPosition{
	//				Id:             binancePositionMapCompare[vReqResData.Symbol+"SHORT"].Id,
	//				Symbol:         binancePositionMapCompare[vReqResData.Symbol+"SHORT"].Symbol,
	//				PositionSide:   binancePositionMapCompare[vReqResData.Symbol+"SHORT"].PositionSide,
	//				PositionAmount: 0,
	//				CreatedAt:      binancePositionMapCompare[vReqResData.Symbol+"SHORT"].CreatedAt,
	//				UpdatedAt:      binancePositionMapCompare[vReqResData.Symbol+"SHORT"].UpdatedAt,
	//			}
	//			binancePositionMapCompare[vReqResData.Symbol+"LONG"] = &entity.TraderPosition{
	//				Id:             binancePositionMapCompare[vReqResData.Symbol+"LONG"].Id,
	//				Symbol:         binancePositionMapCompare[vReqResData.Symbol+"LONG"].Symbol,
	//				PositionSide:   binancePositionMapCompare[vReqResData.Symbol+"LONG"].PositionSide,
	//				PositionAmount: 0,
	//				CreatedAt:      binancePositionMapCompare[vReqResData.Symbol+"LONG"].CreatedAt,
	//				UpdatedAt:      binancePositionMapCompare[vReqResData.Symbol+"LONG"].UpdatedAt,
	//			}
	//
	//			if IsEqual(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount, 0) { // both仓为0
	//				// 认为两仓都无
	//
	//			} else if math.Signbit(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
	//				lastPositionSide = "SHORT"
	//				binancePositionMapCompare[vReqResData.Symbol+"SHORT"].PositionAmount = math.Abs(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount)
	//			} else {
	//				lastPositionSide = "LONG"
	//				binancePositionMapCompare[vReqResData.Symbol+"LONG"].PositionAmount = math.Abs(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount)
	//			}
	//
	//			// 本次仓位
	//			var tmpPositionSide string
	//			if IsEqual(currentAmount, 0) { // 本次仓位是0
	//				if 0 >= len(lastPositionSide) {
	//					// 本次和上一次仓位都是0，应该不会走到这里
	//					fmt.Println("新，仓位异常逻辑，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
	//					continue
	//				}
	//
	//				// 仍为是一次完全平仓，仓位和上一次保持一致
	//				tmpPositionSide = lastPositionSide
	//			} else if math.Signbit(currentAmount) { // 判断有无符号
	//				// 第二步：本次仓位
	//
	//				// 上次和本次相反需要平上次
	//				if "LONG" == lastPositionSide {
	//					orderUpdateData = append(orderUpdateData, &do.TraderPosition{
	//						Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
	//						Symbol:         vReqResData.Symbol,
	//						PositionSide:   lastPositionSide,
	//						PositionAmount: float64(0),
	//					})
	//				}
	//
	//				tmpPositionSide = "SHORT"
	//			} else {
	//				// 第二步：本次仓位
	//
	//				// 上次和本次相反需要平上次
	//				if "SHORT" == lastPositionSide {
	//					orderUpdateData = append(orderUpdateData, &do.TraderPosition{
	//						Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
	//						Symbol:         vReqResData.Symbol,
	//						PositionSide:   lastPositionSide,
	//						PositionAmount: float64(0),
	//					})
	//				}
	//
	//				tmpPositionSide = "LONG"
	//			}
	//
	//			orderUpdateData = append(orderUpdateData, &do.TraderPosition{
	//				Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
	//				Symbol:         vReqResData.Symbol,
	//				PositionSide:   tmpPositionSide,
	//				PositionAmount: currentAmountAbs,
	//			})
	//		}
	//	}
	//}
	//
	//if 0 >= len(insertData) && 0 >= len(updateData) {
	//	return nil
	//}
	//
	//// 时长
	//fmt.Printf("程序拉取部分，开始 %v, 时长: %v, 交易员: %v\n", start, time.Since(start), traderNum)
	//err = g.DB().Transaction(context.TODO(), func(ctx context.Context, tx gdb.TX) error {
	//	batchSize := 500
	//	for i := 0; i < len(insertData); i += batchSize {
	//		end := i + batchSize
	//		if end > len(insertData) {
	//			end = len(insertData)
	//		}
	//		batch := insertData[i:end]
	//
	//		_, err = tx.Ctx(ctx).Insert("trader_position_"+strconv.FormatUint(traderNum, 10), batch)
	//		if nil != err {
	//			return err
	//		}
	//	}
	//
	//	for i := 0; i < len(updateData); i++ {
	//		_, err = tx.Ctx(ctx).Update("trader_position_"+strconv.FormatUint(traderNum, 10), updateData[i], "id", updateData[i].Id)
	//		if nil != err {
	//			return err
	//		}
	//	}
	//
	//	return nil
	//})
	//if nil != err {
	//	fmt.Println("新，更新数据库错误，信息", traderNum, err)
	//	return err
	//}
	//
	//// 推送订单，数据库已初始化仓位，新仓库
	//if 0 >= len(binancePositionMapCompare) {
	//	fmt.Println("初始化仓位成功，交易员信息", traderNum)
	//	return nil
	//}
	//
	//// 数据库必须信息
	//var (
	//	userBindTraders []*entity.NewUserBindTraderTwo
	//	users           []*entity.NewUser
	//)
	//err = g.Model("new_user_bind_trader_two").Ctx(ctx).
	//	Where("trader_id=? and status=? and init_order=?", trader[0].Id, 0, 1).
	//	Scan(&userBindTraders)
	//if nil != err {
	//	return err
	//}
	//
	//userIds := make([]uint, 0)
	//for _, vUserBindTraders := range userBindTraders {
	//	userIds = append(userIds, vUserBindTraders.UserId)
	//}
	//if 0 >= len(userIds) {
	//	fmt.Println("新，无人跟单的交易员下单，信息", traderNum)
	//	return nil
	//}
	//
	//err = g.Model("new_user").Ctx(ctx).
	//	Where("id in(?)", userIds).
	//	Where("api_status =? and bind_trader_status_tfi=? and is_dai=? and use_new_system=?", 1, 1, 1, 2).
	//	Scan(&users)
	//if nil != err {
	//	return err
	//}
	//if 0 >= len(users) {
	//	fmt.Println("新，未查询到用户信息，信息", traderNum)
	//	return nil
	//}
	//
	//// 处理
	//usersMap := make(map[uint]*entity.NewUser, 0)
	//for _, vUsers := range users {
	//	usersMap[vUsers.Id] = vUsers
	//}
	//
	//// 获取代币信息
	//var (
	//	symbols []*entity.LhCoinSymbol
	//)
	//err = g.Model("lh_coin_symbol").Ctx(ctx).Scan(&symbols)
	//if nil != err {
	//	return err
	//}
	//if 0 >= len(symbols) {
	//	fmt.Println("新，空的代币信息，信息", traderNum)
	//	return nil
	//}
	//
	//// 处理
	//symbolsMap := make(map[string]*entity.LhCoinSymbol, 0)
	//for _, vSymbols := range symbols {
	//	symbolsMap[vSymbols.Symbol+"USDT"] = vSymbols
	//}
	//
	//// 获取交易员和用户的最新保证金信息
	//var (
	//	userInfos   []*entity.NewUserInfo
	//	traderInfos []*entity.NewTraderInfo
	//)
	//err = g.Model("new_user_info").Ctx(ctx).Scan(&userInfos)
	//if nil != err {
	//	return err
	//}
	//// 处理
	//userInfosMap := make(map[uint]*entity.NewUserInfo, 0)
	//for _, vUserInfos := range userInfos {
	//	userInfosMap[vUserInfos.UserId] = vUserInfos
	//}
	//
	//err = g.Model("new_trader_info").Ctx(ctx).Scan(&traderInfos)
	//if nil != err {
	//	return err
	//}
	//// 处理
	//traderInfosMap := make(map[uint]*entity.NewTraderInfo, 0)
	//for _, vTraderInfos := range traderInfos {
	//	traderInfosMap[vTraderInfos.TraderId] = vTraderInfos
	//}
	//
	//wg := sync.WaitGroup{}
	//// 遍历跟单者
	//for _, vUserBindTraders := range userBindTraders {
	//	tmpUserBindTraders := vUserBindTraders
	//	if _, ok := usersMap[tmpUserBindTraders.UserId]; !ok {
	//		fmt.Println("新，未匹配到用户信息，用户的信息无效了，信息", traderNum, tmpUserBindTraders)
	//		continue
	//	}
	//
	//	tmpTrader := trader[0]
	//	tmpUsers := usersMap[tmpUserBindTraders.UserId]
	//	if 0 >= len(tmpUsers.ApiSecret) || 0 >= len(tmpUsers.ApiKey) {
	//		fmt.Println("新，用户的信息无效了，信息", traderNum, tmpUserBindTraders)
	//		continue
	//	}
	//
	//	if lessThanOrEqualZero(tmpTrader.BaseMoney, 0, 1e-7) {
	//		fmt.Println("新，交易员信息无效了，信息", tmpTrader, tmpUserBindTraders)
	//		continue
	//	}
	//
	//	// 新增仓位
	//	for _, vInsertData := range orderInsertData {
	//		// 一个新symbol通常3个开仓方向short，long，both，屏蔽一下未真实开仓的
	//		tmpInsertData := vInsertData
	//		if lessThanOrEqualZero(tmpInsertData.PositionAmount.(float64), 0, 1e-7) {
	//			continue
	//		}
	//
	//		if _, ok := symbolsMap[tmpInsertData.Symbol.(string)]; !ok {
	//			fmt.Println("新，代币信息无效，信息", tmpInsertData, tmpUserBindTraders)
	//			continue
	//		}
	//
	//		var (
	//			tmpQty        float64
	//			quantity      string
	//			quantityFloat float64
	//			side          string
	//			positionSide  string
	//			orderType     = "MARKET"
	//		)
	//		if "LONG" == tmpInsertData.PositionSide {
	//			positionSide = "LONG"
	//			side = "BUY"
	//		} else if "SHORT" == tmpInsertData.PositionSide {
	//			positionSide = "SHORT"
	//			side = "SELL"
	//		} else {
	//			fmt.Println("新，无效信息，信息", vInsertData)
	//			continue
	//		}
	//
	//		// 本次 代单员币的数量 * (用户保证金/代单员保证金)
	//		tmpQty = tmpInsertData.PositionAmount.(float64) * float64(tmpUserBindTraders.Amount) / tmpTrader.BaseMoney // 本次开单数量
	//
	//		// todo 目前是松柏系列账户
	//		if _, ok := userInfosMap[tmpUserBindTraders.UserId]; ok {
	//			if _, ok2 := traderInfosMap[tmpUserBindTraders.TraderId]; ok2 {
	//				if 0 < traderInfosMap[tmpUserBindTraders.TraderId].BaseMoney && 0 < userInfosMap[tmpUserBindTraders.UserId].BaseMoney {
	//					tmpQty = tmpInsertData.PositionAmount.(float64) * userInfosMap[tmpUserBindTraders.UserId].BaseMoney / traderInfosMap[tmpUserBindTraders.TraderId].BaseMoney // 本次开单数量
	//				} else {
	//					fmt.Println("新，无效信息base_money1，信息", traderInfosMap[tmpUserBindTraders.TraderId], userInfosMap[tmpUserBindTraders.UserId])
	//				}
	//			}
	//		}
	//
	//		// 精度调整
	//		if 0 >= symbolsMap[tmpInsertData.Symbol.(string)].QuantityPrecision {
	//			quantity = fmt.Sprintf("%d", int64(tmpQty))
	//		} else {
	//			quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap[tmpInsertData.Symbol.(string)].QuantityPrecision, 64)
	//		}
	//
	//		quantityFloat, err = strconv.ParseFloat(quantity, 64)
	//		if nil != err {
	//			fmt.Println(err)
	//			return
	//		}
	//
	//		if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
	//			continue
	//		}
	//
	//		wg.Add(1)
	//		err = s.pool.Add(ctx, func(ctx context.Context) {
	//			defer wg.Done()
	//
	//			// 下单，不用计算数量，新仓位
	//			// 新订单数据
	//			currentOrder := &do.NewUserOrderTwo{
	//				UserId:        tmpUserBindTraders.UserId,
	//				TraderId:      tmpUserBindTraders.TraderId,
	//				Symbol:        tmpInsertData.Symbol.(string),
	//				Side:          side,
	//				PositionSide:  positionSide,
	//				Quantity:      quantityFloat,
	//				Price:         0,
	//				TraderQty:     tmpInsertData.PositionAmount.(float64),
	//				OrderType:     orderType,
	//				ClosePosition: "",
	//				CumQuote:      0,
	//				ExecutedQty:   0,
	//				AvgPrice:      0,
	//			}
	//
	//			var (
	//				binanceOrderRes *binanceOrder
	//				orderInfoRes    *orderInfo
	//			)
	//			// 请求下单
	//			binanceOrderRes, orderInfoRes, err = requestBinanceOrder(tmpInsertData.Symbol.(string), side, orderType, positionSide, quantity, tmpUsers.ApiKey, tmpUsers.ApiSecret)
	//			if nil != err {
	//				fmt.Println(err)
	//				return
	//			}
	//
	//			// 下单异常
	//			if 0 >= binanceOrderRes.OrderId {
	//				// 写入
	//				orderErr := &do.NewUserOrderErrTwo{
	//					UserId:        currentOrder.UserId,
	//					TraderId:      currentOrder.TraderId,
	//					ClientOrderId: "",
	//					OrderId:       "",
	//					Symbol:        currentOrder.Symbol,
	//					Side:          currentOrder.Side,
	//					PositionSide:  currentOrder.PositionSide,
	//					Quantity:      quantityFloat,
	//					Price:         currentOrder.Price,
	//					TraderQty:     currentOrder.TraderQty,
	//					OrderType:     currentOrder.OrderType,
	//					ClosePosition: currentOrder.ClosePosition,
	//					CumQuote:      currentOrder.CumQuote,
	//					ExecutedQty:   currentOrder.ExecutedQty,
	//					AvgPrice:      currentOrder.AvgPrice,
	//					HandleStatus:  currentOrder.HandleStatus,
	//					Code:          orderInfoRes.Code,
	//					Msg:           orderInfoRes.Msg,
	//					Proportion:    0,
	//				}
	//
	//				err = g.DB().Transaction(context.TODO(), func(ctx context.Context, tx gdb.TX) error {
	//					_, err = tx.Ctx(ctx).Insert("new_user_order_err_two", orderErr)
	//					if nil != err {
	//						fmt.Println(err)
	//						return err
	//					}
	//
	//					return nil
	//				})
	//				if nil != err {
	//					fmt.Println("新，下单错误，记录错误信息错误，信息", err, traderNum, vInsertData, tmpUserBindTraders)
	//					return
	//				}
	//
	//				return // 返回
	//			}
	//
	//			currentOrder.OrderId = strconv.FormatInt(binanceOrderRes.OrderId, 10)
	//
	//			currentOrder.CumQuote, err = strconv.ParseFloat(binanceOrderRes.CumQuote, 64)
	//			if nil != err {
	//				fmt.Println("新，下单错误，解析错误1，信息", err, currentOrder, binanceOrderRes)
	//				return
	//			}
	//
	//			currentOrder.ExecutedQty, err = strconv.ParseFloat(binanceOrderRes.ExecutedQty, 64)
	//			if nil != err {
	//				fmt.Println("新，下单错误，解析错误2，信息", err, currentOrder, binanceOrderRes)
	//				return
	//			}
	//
	//			currentOrder.AvgPrice, err = strconv.ParseFloat(binanceOrderRes.AvgPrice, 64)
	//			if nil != err {
	//				fmt.Println("新，下单错误，解析错误3，信息", err, currentOrder, binanceOrderRes)
	//				return
	//			}
	//
	//			// 写入
	//			err = g.DB().Transaction(context.TODO(), func(ctx context.Context, tx gdb.TX) error {
	//				_, err = tx.Ctx(ctx).Insert("new_user_order_two_"+strconv.FormatInt(int64(tmpUserBindTraders.UserId), 10), currentOrder)
	//				if nil != err {
	//					fmt.Println(err)
	//					return err
	//				}
	//
	//				return nil
	//			})
	//			if nil != err {
	//				fmt.Println("新，下单错误，记录信息错误，信息", err, traderNum, vInsertData, tmpUserBindTraders)
	//				return
	//			}
	//
	//			return
	//		})
	//		if nil != err {
	//			fmt.Println("新，添加下单任务异常，新增仓位，错误信息：", err, traderNum, vInsertData, tmpUserBindTraders)
	//		}
	//	}
	//
	//	// 判断需要修改仓位
	//	if 0 >= len(orderUpdateData) {
	//		continue
	//	}
	//
	//	// 获取用户历史仓位
	//	var (
	//		userOrderTwo []*entity.NewUserOrderTwo
	//	)
	//	err = g.Model("new_user_order_two_"+strconv.FormatInt(int64(tmpUserBindTraders.UserId), 10)).Ctx(ctx).
	//		Where("trader_id=?", tmpTrader.Id).
	//		Scan(&userOrderTwo)
	//	if nil != err {
	//		return err
	//	}
	//	userOrderTwoSymbolPositionSideCount := make(map[string]float64, 0)
	//	for _, vUserOrderTwo := range userOrderTwo {
	//		if _, ok := userOrderTwoSymbolPositionSideCount[vUserOrderTwo.Symbol+vUserOrderTwo.PositionSide]; !ok {
	//			userOrderTwoSymbolPositionSideCount[vUserOrderTwo.Symbol+vUserOrderTwo.PositionSide] = 0
	//		}
	//
	//		if "LONG" == vUserOrderTwo.PositionSide {
	//			if "BUY" == vUserOrderTwo.Side {
	//				userOrderTwoSymbolPositionSideCount[vUserOrderTwo.Symbol+vUserOrderTwo.PositionSide] += vUserOrderTwo.ExecutedQty
	//			} else if "SELL" == vUserOrderTwo.Side {
	//				userOrderTwoSymbolPositionSideCount[vUserOrderTwo.Symbol+vUserOrderTwo.PositionSide] -= vUserOrderTwo.ExecutedQty
	//			} else {
	//				fmt.Println("新，历史仓位解析异常1，错误信息：", err, traderNum, vUserOrderTwo, tmpUserBindTraders)
	//				continue
	//			}
	//		} else if "SHORT" == vUserOrderTwo.PositionSide {
	//			if "SELL" == vUserOrderTwo.Side {
	//				userOrderTwoSymbolPositionSideCount[vUserOrderTwo.Symbol+vUserOrderTwo.PositionSide] += vUserOrderTwo.ExecutedQty
	//			} else if "BUY" == vUserOrderTwo.Side {
	//				userOrderTwoSymbolPositionSideCount[vUserOrderTwo.Symbol+vUserOrderTwo.PositionSide] -= vUserOrderTwo.ExecutedQty
	//			} else {
	//				fmt.Println("新，历史仓位解析异常2，错误信息：", err, traderNum, vUserOrderTwo, tmpUserBindTraders)
	//				continue
	//			}
	//		} else {
	//			fmt.Println("新，历史仓位解析异常3，错误信息：", err, traderNum, vUserOrderTwo, tmpUserBindTraders)
	//			continue
	//		}
	//	}
	//
	//	// 修改仓位
	//	for _, vUpdateData := range orderUpdateData {
	//		tmpUpdateData := vUpdateData
	//		if _, ok := binancePositionMapCompare[vUpdateData.Symbol.(string)+vUpdateData.PositionSide.(string)]; !ok {
	//			fmt.Println("新，添加下单任务异常，修改仓位，错误信息：", err, traderNum, vUpdateData, tmpUserBindTraders)
	//			continue
	//		}
	//		lastPositionData := binancePositionMapCompare[vUpdateData.Symbol.(string)+vUpdateData.PositionSide.(string)]
	//
	//		if _, ok := symbolsMap[tmpUpdateData.Symbol.(string)]; !ok {
	//			fmt.Println("新，代币信息无效，信息", tmpUpdateData, tmpUserBindTraders)
	//			continue
	//		}
	//
	//		var (
	//			tmpQty        float64
	//			quantity      string
	//			quantityFloat float64
	//			side          string
	//			positionSide  string
	//			orderType     = "MARKET"
	//		)
	//
	//		if lessThanOrEqualZero(tmpUpdateData.PositionAmount.(float64), 0, 1e-7) {
	//			fmt.Println("新，完全平仓：", tmpUpdateData)
	//			// 全平仓
	//			if "LONG" == tmpUpdateData.PositionSide {
	//				positionSide = "LONG"
	//				side = "SELL"
	//			} else if "SHORT" == tmpUpdateData.PositionSide {
	//				positionSide = "SHORT"
	//				side = "BUY"
	//			} else {
	//				fmt.Println("新，无效信息，信息", tmpUpdateData)
	//				continue
	//			}
	//
	//			// 未开启过仓位
	//			if _, ok := userOrderTwoSymbolPositionSideCount[tmpUpdateData.Symbol.(string)+tmpUpdateData.PositionSide.(string)]; !ok {
	//				continue
	//			}
	//
	//			// 认为是0
	//			if lessThanOrEqualZero(userOrderTwoSymbolPositionSideCount[tmpUpdateData.Symbol.(string)+tmpUpdateData.PositionSide.(string)], 0, 1e-7) {
	//				continue
	//			}
	//
	//			// 剩余仓位
	//			tmpQty = userOrderTwoSymbolPositionSideCount[tmpUpdateData.Symbol.(string)+tmpUpdateData.PositionSide.(string)]
	//		} else if lessThanOrEqualZero(lastPositionData.PositionAmount, tmpUpdateData.PositionAmount.(float64), 1e-7) {
	//			fmt.Println("新，追加仓位：", tmpUpdateData, lastPositionData)
	//			// 本次加仓 代单员币的数量 * (用户保证金/代单员保证金)
	//			if "LONG" == tmpUpdateData.PositionSide {
	//				positionSide = "LONG"
	//				side = "BUY"
	//			} else if "SHORT" == tmpUpdateData.PositionSide {
	//				positionSide = "SHORT"
	//				side = "SELL"
	//			} else {
	//				fmt.Println("新，无效信息，信息", tmpUpdateData)
	//				continue
	//			}
	//
	//			// 本次减去上一次
	//			tmpQty = (tmpUpdateData.PositionAmount.(float64) - lastPositionData.PositionAmount) * float64(tmpUserBindTraders.Amount) / tmpTrader.BaseMoney // 本次开单数量
	//			if _, ok := userInfosMap[tmpUserBindTraders.UserId]; ok {
	//				if _, ok2 := traderInfosMap[tmpUserBindTraders.TraderId]; ok2 {
	//					if 0 < traderInfosMap[tmpUserBindTraders.TraderId].BaseMoney && 0 < userInfosMap[tmpUserBindTraders.UserId].BaseMoney {
	//						tmpQty = (tmpUpdateData.PositionAmount.(float64) - lastPositionData.PositionAmount) * userInfosMap[tmpUserBindTraders.UserId].BaseMoney / traderInfosMap[tmpUserBindTraders.TraderId].BaseMoney // 本次开单数量
	//					} else {
	//						fmt.Println("新，无效信息base_money2，信息", traderInfosMap[tmpUserBindTraders.TraderId], userInfosMap[tmpUserBindTraders.UserId])
	//					}
	//				}
	//			}
	//
	//		} else if lessThanOrEqualZero(tmpUpdateData.PositionAmount.(float64), lastPositionData.PositionAmount, 1e-7) {
	//			fmt.Println("新，部分平仓：", tmpUpdateData, lastPositionData)
	//			// 部分平仓
	//			if "LONG" == tmpUpdateData.PositionSide {
	//				positionSide = "LONG"
	//				side = "SELL"
	//			} else if "SHORT" == tmpUpdateData.PositionSide {
	//				positionSide = "SHORT"
	//				side = "BUY"
	//			} else {
	//				fmt.Println("新，无效信息，信息", tmpUpdateData)
	//				continue
	//			}
	//
	//			// 未开启过仓位
	//			if _, ok := userOrderTwoSymbolPositionSideCount[tmpUpdateData.Symbol.(string)+tmpUpdateData.PositionSide.(string)]; !ok {
	//				continue
	//			}
	//
	//			// 认为是0
	//			if lessThanOrEqualZero(userOrderTwoSymbolPositionSideCount[tmpUpdateData.Symbol.(string)+tmpUpdateData.PositionSide.(string)], 0, 1e-7) {
	//				continue
	//			}
	//
	//			// 上次仓位
	//			if lessThanOrEqualZero(lastPositionData.PositionAmount, 0, 1e-7) {
	//				fmt.Println("新，部分平仓，上次仓位信息无效，信息", lastPositionData, tmpUpdateData)
	//				continue
	//			}
	//
	//			// 按百分比
	//			tmpQty = userOrderTwoSymbolPositionSideCount[tmpUpdateData.Symbol.(string)+tmpUpdateData.PositionSide.(string)] * (lastPositionData.PositionAmount - tmpUpdateData.PositionAmount.(float64)) / lastPositionData.PositionAmount
	//		} else {
	//			fmt.Println("新，分析仓位无效，信息", lastPositionData, tmpUpdateData)
	//			continue
	//		}
	//
	//		// 精度调整
	//		if 0 >= symbolsMap[tmpUpdateData.Symbol.(string)].QuantityPrecision {
	//			quantity = fmt.Sprintf("%d", int64(tmpQty))
	//		} else {
	//			quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap[tmpUpdateData.Symbol.(string)].QuantityPrecision, 64)
	//		}
	//
	//		quantityFloat, err = strconv.ParseFloat(quantity, 64)
	//		if nil != err {
	//			fmt.Println(err)
	//			return
	//		}
	//
	//		if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
	//			continue
	//		}
	//
	//		fmt.Println("新，下单数字，信息:", quantity, quantityFloat)
	//
	//		wg.Add(1)
	//		err = s.pool.Add(ctx, func(ctx context.Context) {
	//			defer wg.Done()
	//
	//			// 下单，不用计算数量，新仓位
	//			// 新订单数据
	//			currentOrder := &do.NewUserOrderTwo{
	//				UserId:        tmpUserBindTraders.UserId,
	//				TraderId:      tmpUserBindTraders.TraderId,
	//				Symbol:        tmpUpdateData.Symbol.(string),
	//				Side:          side,
	//				PositionSide:  positionSide,
	//				Quantity:      quantityFloat,
	//				Price:         0,
	//				TraderQty:     quantityFloat,
	//				OrderType:     orderType,
	//				ClosePosition: "",
	//				CumQuote:      0,
	//				ExecutedQty:   0,
	//				AvgPrice:      0,
	//			}
	//
	//			var (
	//				binanceOrderRes *binanceOrder
	//				orderInfoRes    *orderInfo
	//			)
	//			// 请求下单
	//			binanceOrderRes, orderInfoRes, err = requestBinanceOrder(tmpUpdateData.Symbol.(string), side, orderType, positionSide, quantity, tmpUsers.ApiKey, tmpUsers.ApiSecret)
	//			if nil != err {
	//				fmt.Println(err)
	//				return
	//			}
	//
	//			// 下单异常
	//			if 0 >= binanceOrderRes.OrderId {
	//				// 写入
	//				orderErr := &do.NewUserOrderErrTwo{
	//					UserId:        currentOrder.UserId,
	//					TraderId:      currentOrder.TraderId,
	//					ClientOrderId: "",
	//					OrderId:       "",
	//					Symbol:        currentOrder.Symbol,
	//					Side:          currentOrder.Side,
	//					PositionSide:  currentOrder.PositionSide,
	//					Quantity:      quantityFloat,
	//					Price:         currentOrder.Price,
	//					TraderQty:     currentOrder.TraderQty,
	//					OrderType:     currentOrder.OrderType,
	//					ClosePosition: currentOrder.ClosePosition,
	//					CumQuote:      currentOrder.CumQuote,
	//					ExecutedQty:   currentOrder.ExecutedQty,
	//					AvgPrice:      currentOrder.AvgPrice,
	//					HandleStatus:  currentOrder.HandleStatus,
	//					Code:          orderInfoRes.Code,
	//					Msg:           orderInfoRes.Msg,
	//					Proportion:    0,
	//				}
	//
	//				err = g.DB().Transaction(context.TODO(), func(ctx context.Context, tx gdb.TX) error {
	//					_, err = tx.Ctx(ctx).Insert("new_user_order_err_two", orderErr)
	//					if nil != err {
	//						fmt.Println(err)
	//						return err
	//					}
	//
	//					return nil
	//				})
	//				if nil != err {
	//					fmt.Println("新，下单错误，记录错误信息错误，信息", err, traderNum, tmpUpdateData, tmpUserBindTraders)
	//					return
	//				}
	//
	//				return // 返回
	//			}
	//
	//			currentOrder.OrderId = strconv.FormatInt(binanceOrderRes.OrderId, 10)
	//
	//			currentOrder.CumQuote, err = strconv.ParseFloat(binanceOrderRes.CumQuote, 64)
	//			if nil != err {
	//				fmt.Println("新，下单错误，解析错误1，信息", err, currentOrder, binanceOrderRes)
	//				return
	//			}
	//
	//			currentOrder.ExecutedQty, err = strconv.ParseFloat(binanceOrderRes.ExecutedQty, 64)
	//			if nil != err {
	//				fmt.Println("新，下单错误，解析错误2，信息", err, currentOrder, binanceOrderRes)
	//				return
	//			}
	//
	//			currentOrder.AvgPrice, err = strconv.ParseFloat(binanceOrderRes.AvgPrice, 64)
	//			if nil != err {
	//				fmt.Println("新，下单错误，解析错误3，信息", err, currentOrder, binanceOrderRes)
	//				return
	//			}
	//
	//			// 写入
	//			err = g.DB().Transaction(context.TODO(), func(ctx context.Context, tx gdb.TX) error {
	//				_, err = tx.Ctx(ctx).Insert("new_user_order_two_"+strconv.FormatInt(int64(tmpUserBindTraders.UserId), 10), currentOrder)
	//				if nil != err {
	//					fmt.Println(err)
	//					return err
	//				}
	//
	//				return nil
	//			})
	//			if nil != err {
	//				fmt.Println("新，下单错误，记录信息错误，信息", err, traderNum, tmpUpdateData, tmpUserBindTraders)
	//				return
	//			}
	//		})
	//		if nil != err {
	//			fmt.Println("新，添加下单任务异常，修改仓位，错误信息：", err, traderNum, vUpdateData, tmpUserBindTraders)
	//		}
	//	}
	//
	//}
	//// 回收协程
	//wg.Wait()

	return nil
}

var (
	globalTraderNum = uint64(4150465500682762240) // todo 改 3887627985594221568
	orderMap        = gmap.New(true)              // 初始化下单记录
	orderErr        = gset.New(true)

	baseMoneyGuiTu      = gtype.NewFloat64()
	baseMoneyUserAllMap = gmap.NewIntAnyMap(true)

	globalUsers = gmap.New(true)

	// 仓位
	binancePositionMap = make(map[string]*entity.TraderPosition, 0)

	symbolsMap     = gmap.NewStrAnyMap(true)
	coins          []string
	symbolsMapGate = gmap.NewStrAnyMap(true)

	// 币种日线收盘价
	initPrice            = gmap.NewStrAnyMap(true)
	btcUsdtOrder         = gtype.NewFloat64()
	coinUsdtOrder        = gtype.NewFloat64()
	HandleKLineApiKey    = ""
	HandleKLineApiSecret = ""
	//HandleKLineApiKeyTwo    = ""
	//HandleKLineApiSecretTwo = ""
)

// GetGlobalInfo 获取全局测试数据
func (s *sBinanceTraderHistory) GetGlobalInfo(ctx context.Context) {
	// 遍历map
	orderMap.Iterator(func(k interface{}, v interface{}) bool {
		fmt.Println("龟兔，用户下单，测试结果:", k, v)
		return true
	})

	orderErr.Iterator(func(v interface{}) bool {
		fmt.Println("龟兔，用户下单，测试结果，错误单:", v)
		return true
	})

	fmt.Println("龟兔，保证金，测试结果:", baseMoneyGuiTu)
	baseMoneyUserAllMap.Iterator(func(k int, v interface{}) bool {
		fmt.Println("龟兔，保证金，用户，测试结果:", k, v)
		return true
	})

	globalUsers.Iterator(func(k interface{}, v interface{}) bool {
		fmt.Println("龟兔，用户信息:", v.(*entity.NewUser))
		return true
	})

	for _, vBinancePositionMap := range binancePositionMap {
		if IsEqual(vBinancePositionMap.PositionAmount, 0) {
			continue
		}

		fmt.Println("龟兔，带单员仓位，信息:", vBinancePositionMap)
	}
}

//// InitGlobalInfo 初始化信息
//func (s *sBinanceTraderHistory) InitGlobalInfo(ctx context.Context) bool {
//	var (
//		err          error
//		keyPositions []*entity.KeyPosition
//	)
//	err = g.Model("key_position").Ctx(ctx).Where("amount>", 0).Scan(&keyPositions)
//	if nil != err {
//		fmt.Println("龟兔，初始化仓位，数据库查询错误：", err)
//		return false
//	}
//
//	for _, vKeyPositions := range keyPositions {
//		orderMap.Set(vKeyPositions.Key, vKeyPositions.Amount)
//	}
//
//	return true
//}

type SymbolGate struct {
	Symbol           string  `json:"symbol"            ` //
	QuantoMultiplier float64 `json:"quantityPrecision" ` //
	OrderPriceRound  int
}

// InitCoinInfo 初始化信息
func (s *sBinanceTraderHistory) InitCoinInfo(ctx context.Context) bool {
	var (
		info *ExchangeInfo
		err  error
	)

	info, err = getBinanceExchangeInfo()
	if err != nil {
		fmt.Println("获取交易信息失败:", err)
		return false
	}

	for _, v := range info.Symbols {
		if "BTC" != v.QuoteAsset {
			continue
		}

		coins = append(coins, v.BaseAsset)
	}

	return true
}

// UpdateCoinInfo 初始化信息
func (s *sBinanceTraderHistory) UpdateCoinInfo(ctx context.Context) bool {
	//// 获取代币信息
	//var (
	//	err     error
	//	symbols []*entity.LhCoinSymbol
	//)
	//err = g.Model("lh_coin_symbol").Ctx(ctx).Scan(&symbols)
	//if nil != err || 0 >= len(symbols) {
	//	fmt.Println("龟兔，初始化，币种，数据库查询错误：", err)
	//	return false
	//}
	//// 处理
	//for _, vSymbols := range symbols {
	//	symbolsMap.Set(vSymbols.Symbol+"USDT", vSymbols)
	//}
	//
	//return true

	// 获取代币信息
	var (
		err               error
		binanceSymbolInfo []*BinanceSymbolInfo
	)
	binanceSymbolInfo, err = getBinanceFuturesPairs()
	if nil != err {
		log.Println("更新币种，binance", err)
		return false
	}

	for _, v := range binanceSymbolInfo {
		symbolsMap.Set(v.Symbol, &entity.LhCoinSymbol{
			Id:                0,
			Coin:              v.BaseAsset,
			Symbol:            v.Symbol,
			StartTime:         0,
			EndTime:           0,
			PricePrecision:    v.PricePrecision,
			QuantityPrecision: v.QuantityPrecision,
			IsOpen:            0,
		})
	}

	//var (
	//	resGate []gateapi.Contract
	//)
	//
	//resGate, err = getGateContract()
	//if nil != err {
	//	log.Println("更新币种， gate", err)
	//	return false
	//}
	//
	//for _, v := range resGate {
	//	var (
	//		tmp  float64
	//		tmp2 int
	//	)
	//	tmp, err = strconv.ParseFloat(v.QuantoMultiplier, 64)
	//	if nil != err {
	//		continue
	//	}
	//
	//	tmp2 = getDecimalPlaces(v.OrderPriceRound)
	//
	//	base := strings.TrimSuffix(v.Name, "_USDT")
	//	symbolsMapGate.Set(base+"USDT", &SymbolGate{
	//		Symbol:           v.Name,
	//		QuantoMultiplier: tmp,
	//		OrderPriceRound:  tmp2,
	//	})
	//}

	return true
}

func getDecimalPlaces(orderPriceRound string) int {
	parts := strings.Split(orderPriceRound, ".")
	if len(parts) == 2 {
		// 去除末尾多余的 0
		decimals := strings.TrimRight(parts[1], "0")
		return len(decimals)
	}
	return 0
}

// UpdateKeyPosition 更新keyPosition信息
func (s *sBinanceTraderHistory) UpdateKeyPosition(ctx context.Context) bool {
	var (
		err error
	)

	keyPositionsNew := make(map[string]float64, 0)
	orderMap.Iterator(func(k interface{}, v interface{}) bool {
		keyPositionsNew[k.(string)] = v.(float64)
		return true
	})

	if 0 >= len(keyPositionsNew) {
		return true
	}

	var (
		keyPositions []*entity.KeyPosition
	)
	err = g.Model("key_position").Ctx(ctx).Scan(&keyPositions)
	if nil != err {
		fmt.Println("龟兔，初始化仓位，数据库查询错误：", err)
		return true
	}

	keyPositionsMap := make(map[string]*entity.KeyPosition, 0)
	for _, vKeyPositions := range keyPositions {
		keyPositionsMap[vKeyPositions.Key] = vKeyPositions
	}

	_, err = g.Model("key_position").Ctx(ctx).Where("amount>", 0).Data("amount", 0).Update()
	if nil != err {
		fmt.Println("龟兔，key_position，数据库清0：", err)
	}

	for k, v := range keyPositionsNew {
		if _, ok := keyPositionsMap[k]; ok {
			_, err = g.Model("key_position").Ctx(ctx).Data("amount", v).Where("key", k).Update()
			if nil != err {
				fmt.Println("龟兔，key_position，数据库更新：", err)
			}
		} else {
			_, err = g.Model("key_position").Ctx(ctx).Insert(&do.KeyPosition{
				Key:    k,
				Amount: v,
			})

			if nil != err {
				fmt.Println("龟兔，key_position，数据库写入：", err)
			}
		}
	}

	return true
}

// InitGlobalInfo 初始化信息
func (s *sBinanceTraderHistory) InitGlobalInfo(ctx context.Context) bool {
	//// 获取代币信息
	//var (
	//	err error
	//)
	//
	//// 初始化，恢复仓位数据
	//var (
	//	users []*entity.NewUser
	//)
	//err = g.Model("new_user").Ctx(ctx).
	//	Where("api_status =? and bind_trader_status_tfi=? and is_dai=? and use_new_system=?", 1, 1, 1, 2).
	//	Scan(&users)
	//if nil != err {
	//	fmt.Println("龟兔，初始化，数据库查询错误：", err)
	//	return false
	//}
	//
	//// 执行
	//var (
	//	retry           = false
	//	retryTimes      = 0
	//	retryTimesLimit = 5 // 重试次数
	//	cookieErr       bool
	//	reqResData      []*binancePositionDataList
	//)
	//
	//for _, vUsers := range users {
	//	strUserId := strconv.FormatUint(uint64(vUsers.Id), 10)
	//
	//	for retryTimes < retryTimesLimit { // 最大重试
	//		// 龟兔的数据
	//		reqResData, retry, _ = s.requestBinancePositionHistoryNew(uint64(vUsers.BinanceId), "", "")
	//
	//		// 需要重试
	//		if retry {
	//			retryTimes++
	//			time.Sleep(time.Second * 5)
	//			fmt.Println("龟兔，重试：", retry)
	//			continue
	//		}
	//
	//		// cookie不好使
	//		if 0 >= len(reqResData) {
	//			retryTimes++
	//			cookieErr = true
	//			continue
	//		} else {
	//			cookieErr = false
	//			break
	//		}
	//	}
	//
	//	// cookie 错误
	//	if cookieErr {
	//		fmt.Println("龟兔，初始化，查询仓位，cookie错误")
	//		return false
	//	}
	//
	//	for _, vReqResData := range reqResData {
	//		// 新增
	//		var (
	//			currentAmount    float64
	//			currentAmountAbs float64
	//		)
	//		currentAmount, err = strconv.ParseFloat(vReqResData.PositionAmount, 64)
	//		if nil != err {
	//			fmt.Println("龟兔，初始化，解析金额出错，信息", vReqResData, currentAmount)
	//			return false
	//		}
	//		currentAmountAbs = math.Abs(currentAmount) // 绝对值
	//
	//		if lessThanOrEqualZero(currentAmountAbs, 0, 1e-7) {
	//			continue
	//		}
	//
	//		fmt.Println("龟兔，初始化，用户现在的仓位，信息", vReqResData.Symbol+vReqResData.PositionSide+strUserId, currentAmountAbs)
	//		orderMap.Set(vReqResData.Symbol+"&"+vReqResData.PositionSide+"&"+strUserId, currentAmountAbs)
	//	}
	//
	//	time.Sleep(30 * time.Millisecond)
	//}

	return true
}

// PullAndSetBaseMoneyNewGuiTuAndUser 拉取binance保证金数据
func (s *sBinanceTraderHistory) PullAndSetBaseMoneyNewGuiTuAndUser(ctx context.Context) {
	var (
		err error
		//one string
	)

	//one, err = requestBinanceTraderDetail(globalTraderNum)
	//if nil != err {
	//	fmt.Println("龟兔，拉取保证金失败：", err, globalTraderNum)
	//}
	//if 0 < len(one) {
	//	var tmp float64
	//	tmp, err = strconv.ParseFloat(one, 64)
	//	if nil != err {
	//		fmt.Println("龟兔，拉取保证金，转化失败：", err, globalTraderNum)
	//	}
	//
	//	if !IsEqual(tmp, baseMoneyGuiTu.Val()) {
	//		fmt.Println("龟兔，变更保证金")
	//		baseMoneyGuiTu.Set(tmp)
	//	}
	//}

	var (
		users []*entity.NewUser
	)
	err = g.Model("new_user").Ctx(ctx).
		Where("api_status =? and bind_trader_status_tfi=? and is_dai=? and use_new_system=?", 1, 1, 1, 2).
		Scan(&users)
	if nil != err {
		fmt.Println("龟兔，新增用户，数据库查询错误：", err)
		return
	}

	tmpUserMap := make(map[uint]*entity.NewUser, 0)
	for _, vUsers := range users {
		tmpUserMap[vUsers.Id] = vUsers
	}

	globalUsers.Iterator(func(k interface{}, v interface{}) bool {
		vGlobalUsers := v.(*entity.NewUser)

		if _, ok := tmpUserMap[vGlobalUsers.Id]; !ok {
			fmt.Println("龟兔，变更保证金，用户数据错误，数据库不存在：", vGlobalUsers)
			return true
		}

		tmp := tmpUserMap[vGlobalUsers.Id].Num
		if !baseMoneyUserAllMap.Contains(int(vGlobalUsers.Id)) {
			fmt.Println("初始化成功保证金", vGlobalUsers, tmp, tmpUserMap[vGlobalUsers.Id].Num)
			baseMoneyUserAllMap.Set(int(vGlobalUsers.Id), tmp)
		} else {
			if !IsEqual(tmp, baseMoneyUserAllMap.Get(int(vGlobalUsers.Id)).(float64)) {
				fmt.Println("变更成功", int(vGlobalUsers.Id), tmp, tmpUserMap[vGlobalUsers.Id].Num)
				baseMoneyUserAllMap.Set(int(vGlobalUsers.Id), tmp)
			}
		}

		return true
	})
}

// InsertGlobalUsers  新增用户
func (s *sBinanceTraderHistory) InsertGlobalUsers(ctx context.Context) {
	//var (
	//	err   error
	//	users []*entity.NewUser
	//)
	//err = g.Model("new_user").Ctx(ctx).
	//	Where("api_status =? and bind_trader_status_tfi=? and is_dai=? and use_new_system=?", 1, 1, 1, 2).
	//	Scan(&users)
	//if nil != err {
	//	fmt.Println("龟兔，新增用户，数据库查询错误：", err)
	//	return
	//}
	//
	//tmpUserMap := make(map[uint]*entity.NewUser, 0)
	//for _, vUsers := range users {
	//	tmpUserMap[vUsers.Id] = vUsers
	//}
	//
	//// 第一遍比较，新增
	//for k, vTmpUserMap := range tmpUserMap {
	//	if globalUsers.Contains(k) {
	//		continue
	//	}
	//
	//	// 初始化仓位
	//	fmt.Println("龟兔，新增用户:", k, vTmpUserMap)
	//	if 1 == vTmpUserMap.NeedInit {
	//		_, err = g.Model("new_user").Ctx(ctx).Data("need_init", 0).Where("id=?", vTmpUserMap.Id).Update()
	//		if nil != err {
	//			fmt.Println("龟兔，新增用户，更新初始化状态失败:", k, vTmpUserMap)
	//		}
	//
	//		strUserId := strconv.FormatUint(uint64(vTmpUserMap.Id), 10)
	//
	//		if lessThanOrEqualZero(vTmpUserMap.Num, 0, 1e-7) {
	//			fmt.Println("龟兔，新增用户，保证金系数错误：", vTmpUserMap)
	//			continue
	//		}
	//
	//		// 新增仓位
	//		tmpTraderBaseMoney := baseMoneyGuiTu.Val()
	//		if lessThanOrEqualZero(tmpTraderBaseMoney, 0, 1e-7) {
	//			fmt.Println("龟兔，新增用户，交易员信息无效了，信息", vTmpUserMap)
	//			continue
	//		}
	//
	//		// 获取保证金
	//		var tmpUserBindTradersAmount float64
	//
	//		var (
	//			detail string
	//		)
	//		detail, err = requestBinanceTraderDetail(uint64(vTmpUserMap.BinanceId))
	//		if nil != err {
	//			fmt.Println("龟兔，新增用户，拉取保证金失败：", err, vTmpUserMap)
	//		}
	//		if 0 < len(detail) {
	//			var tmp float64
	//			tmp, err = strconv.ParseFloat(detail, 64)
	//			if nil != err {
	//				fmt.Println("龟兔，新增用户，拉取保证金，转化失败：", err, vTmpUserMap)
	//			}
	//
	//			tmp *= vTmpUserMap.Num
	//			tmpUserBindTradersAmount = tmp
	//			if !baseMoneyUserAllMap.Contains(int(vTmpUserMap.Id)) {
	//				fmt.Println("新增用户，初始化成功保证金", vTmpUserMap, tmp, vTmpUserMap.Num)
	//				baseMoneyUserAllMap.Set(int(vTmpUserMap.Id), tmp)
	//			} else {
	//				if !IsEqual(tmp, baseMoneyUserAllMap.Get(int(vTmpUserMap.Id)).(float64)) {
	//					fmt.Println("新增用户，变更成功", int(vTmpUserMap.Id), tmp, vTmpUserMap.Num)
	//					baseMoneyUserAllMap.Set(int(vTmpUserMap.Id), tmp)
	//				}
	//			}
	//		}
	//
	//		if lessThanOrEqualZero(tmpUserBindTradersAmount, 0, 1e-7) {
	//			fmt.Println("龟兔，新增用户，保证金不足为0：", tmpUserBindTradersAmount, vTmpUserMap.Id)
	//			continue
	//		}
	//
	//		// 仓位
	//		for _, vInsertData := range binancePositionMap {
	//			// 一个新symbol通常3个开仓方向short，long，both，屏蔽一下未真实开仓的
	//			tmpInsertData := vInsertData
	//			if IsEqual(tmpInsertData.PositionAmount, 0) {
	//				continue
	//			}
	//
	//			if !symbolsMap.Contains(tmpInsertData.Symbol) {
	//				fmt.Println("龟兔，新增用户，代币信息无效，信息", tmpInsertData, vTmpUserMap)
	//				continue
	//			}
	//
	//			var (
	//				tmpQty        float64
	//				quantity      string
	//				quantityFloat float64
	//				side          string
	//				positionSide  string
	//				orderType     = "MARKET"
	//			)
	//			if "LONG" == tmpInsertData.PositionSide {
	//				positionSide = "LONG"
	//				side = "BUY"
	//			} else if "SHORT" == tmpInsertData.PositionSide {
	//				positionSide = "SHORT"
	//				side = "SELL"
	//			} else if "BOTH" == tmpInsertData.PositionSide {
	//				if math.Signbit(tmpInsertData.PositionAmount) {
	//					positionSide = "SHORT"
	//					side = "SELL"
	//				} else {
	//					positionSide = "LONG"
	//					side = "BUY"
	//				}
	//			} else {
	//				fmt.Println("龟兔，新增用户，无效信息，信息", vInsertData)
	//				continue
	//			}
	//			tmpPositionAmount := math.Abs(tmpInsertData.PositionAmount)
	//			// 本次 代单员币的数量 * (用户保证金/代单员保证金)
	//			tmpQty = tmpPositionAmount * tmpUserBindTradersAmount / tmpTraderBaseMoney // 本次开单数量
	//
	//			// 精度调整
	//			if 0 >= symbolsMap.Get(tmpInsertData.Symbol).(*entity.LhCoinSymbol).QuantityPrecision {
	//				quantity = fmt.Sprintf("%d", int64(tmpQty))
	//			} else {
	//				quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(tmpInsertData.Symbol).(*entity.LhCoinSymbol).QuantityPrecision, 64)
	//			}
	//
	//			quantityFloat, err = strconv.ParseFloat(quantity, 64)
	//			if nil != err {
	//				fmt.Println(err)
	//				continue
	//			}
	//
	//			if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
	//				continue
	//			}
	//
	//			// 下单，不用计算数量，新仓位
	//			// 新订单数据
	//			currentOrder := &entity.NewUserOrderTwo{
	//				UserId:        vTmpUserMap.Id,
	//				TraderId:      1,
	//				Symbol:        tmpInsertData.Symbol,
	//				Side:          side,
	//				PositionSide:  positionSide,
	//				Quantity:      quantityFloat,
	//				Price:         0,
	//				TraderQty:     tmpPositionAmount,
	//				OrderType:     orderType,
	//				ClosePosition: "",
	//				CumQuote:      0,
	//				ExecutedQty:   0,
	//				AvgPrice:      0,
	//			}
	//
	//			var (
	//				binanceOrderRes *binanceOrder
	//				orderInfoRes    *orderInfo
	//			)
	//			// 请求下单
	//			binanceOrderRes, orderInfoRes, err = requestBinanceOrder(tmpInsertData.Symbol, side, orderType, positionSide, quantity, vTmpUserMap.ApiKey, vTmpUserMap.ApiSecret)
	//			if nil != err {
	//				fmt.Println(err)
	//			}
	//
	//			//binanceOrderRes = &binanceOrder{
	//			//	OrderId:       1,
	//			//	ExecutedQty:   quantity,
	//			//	ClientOrderId: "",
	//			//	Symbol:        "",
	//			//	AvgPrice:      "",
	//			//	CumQuote:      "",
	//			//	Side:          "",
	//			//	PositionSide:  "",
	//			//	ClosePosition: false,
	//			//	Type:          "",
	//			//	Status:        "",
	//			//}
	//
	//			// 下单异常
	//			if 0 >= binanceOrderRes.OrderId {
	//				orderErr.Add(&entity.NewUserOrderErrTwo{
	//					UserId:        currentOrder.UserId,
	//					TraderId:      currentOrder.TraderId,
	//					ClientOrderId: "",
	//					OrderId:       "",
	//					Symbol:        currentOrder.Symbol,
	//					Side:          currentOrder.Side,
	//					PositionSide:  currentOrder.PositionSide,
	//					Quantity:      quantityFloat,
	//					Price:         currentOrder.Price,
	//					TraderQty:     currentOrder.TraderQty,
	//					OrderType:     currentOrder.OrderType,
	//					ClosePosition: currentOrder.ClosePosition,
	//					CumQuote:      currentOrder.CumQuote,
	//					ExecutedQty:   currentOrder.ExecutedQty,
	//					AvgPrice:      currentOrder.AvgPrice,
	//					HandleStatus:  currentOrder.HandleStatus,
	//					Code:          int(orderInfoRes.Code),
	//					Msg:           orderInfoRes.Msg,
	//					Proportion:    0,
	//				})
	//
	//				continue
	//			}
	//
	//			var tmpExecutedQty float64
	//			tmpExecutedQty, err = strconv.ParseFloat(binanceOrderRes.ExecutedQty, 64)
	//			if nil != err {
	//				fmt.Println("龟兔，新增用户，下单错误，解析错误2，信息", err, currentOrder, binanceOrderRes)
	//				continue
	//			}
	//
	//			// 不存在新增，这里只能是开仓
	//			if !orderMap.Contains(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId) {
	//				orderMap.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
	//			} else {
	//				tmpExecutedQty += orderMap.Get(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId).(float64)
	//				orderMap.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
	//			}
	//		}
	//
	//	}
	//
	//	globalUsers.Set(k, vTmpUserMap)
	//}
	//
	//// 第二遍比较，删除
	//tmpIds := make([]uint, 0)
	//globalUsers.Iterator(func(k interface{}, v interface{}) bool {
	//	if _, ok := tmpUserMap[k.(uint)]; !ok {
	//		tmpIds = append(tmpIds, k.(uint))
	//	}
	//	return true
	//})
	//
	//// 删除的人
	//for _, vTmpIds := range tmpIds {
	//	fmt.Println("龟兔，删除用户:", vTmpIds)
	//	globalUsers.Remove(vTmpIds)
	//
	//	tmpRemoveUserKey := make([]string, 0)
	//	// 遍历map
	//	orderMap.Iterator(func(k interface{}, v interface{}) bool {
	//		parts := strings.Split(k.(string), "&")
	//		if 3 != len(parts) {
	//			return true
	//		}
	//
	//		var (
	//			uid uint64
	//		)
	//		uid, err = strconv.ParseUint(parts[2], 10, 64)
	//		if nil != err {
	//			fmt.Println("龟兔，删除用户,解析id错误:", vTmpIds)
	//		}
	//
	//		if uid != uint64(vTmpIds) {
	//			return true
	//		}
	//
	//		tmpRemoveUserKey = append(tmpRemoveUserKey, k.(string))
	//		return true
	//	})
	//
	//	for _, vK := range tmpRemoveUserKey {
	//		if orderMap.Contains(vK) {
	//			orderMap.Remove(vK)
	//		}
	//	}
	//}
}

// InsertGlobalUsersNew  新增用户
func (s *sBinanceTraderHistory) InsertGlobalUsersNew(ctx context.Context) {
	var (
		err   error
		users []*entity.NewUser
	)
	err = g.Model("new_user").Ctx(ctx).
		Where("api_status =? and bind_trader_status_tfi=? and is_dai=? and use_new_system=?", 1, 1, 1, 2).
		Scan(&users)
	if nil != err {
		fmt.Println("龟兔，新增用户，数据库查询错误：", err)
		return
	}

	tmpUserMap := make(map[uint]*entity.NewUser, 0)
	for _, vUsers := range users {
		tmpUserMap[vUsers.Id] = vUsers
	}

	// 第一遍比较，新增
	for k, vTmpUserMap := range tmpUserMap {
		if globalUsers.Contains(k) {
			continue
		}

		tmp := vTmpUserMap.Num
		if !baseMoneyUserAllMap.Contains(int(vTmpUserMap.Id)) {
			fmt.Println("新增用户，初始化成功保证金", vTmpUserMap, tmp, vTmpUserMap.Num)
			baseMoneyUserAllMap.Set(int(vTmpUserMap.Id), tmp)
		} else {
			if !IsEqual(tmp, baseMoneyUserAllMap.Get(int(vTmpUserMap.Id)).(float64)) {
				fmt.Println("新增用户，变更成功", int(vTmpUserMap.Id), tmp, vTmpUserMap.Num)
				baseMoneyUserAllMap.Set(int(vTmpUserMap.Id), tmp)
			}
		}

		fmt.Println("龟兔，新增用户:", k, vTmpUserMap)
		globalUsers.Set(k, vTmpUserMap)
	}

	// 第二遍比较，删除
	tmpIds := make([]uint, 0)
	globalUsers.Iterator(func(k interface{}, v interface{}) bool {
		if _, ok := tmpUserMap[k.(uint)]; !ok {
			tmpIds = append(tmpIds, k.(uint))
		}
		return true
	})

	// 删除的人
	for _, vTmpIds := range tmpIds {
		globalUsers.Remove(vTmpIds)
		fmt.Println("删除:", vTmpIds)
	}
}

// PullAndOrderNewGuiTu 拉取binance数据，仓位，根据cookie 龟兔赛跑
func (s *sBinanceTraderHistory) PullAndOrderNewGuiTu(ctx context.Context) {
	var (
		traderNum                 = globalTraderNum // 龟兔
		zyTraderCookie            []*entity.ZyTraderCookie
		binancePositionMapCompare map[string]*entity.TraderPosition
		reqResData                []*binancePositionDataList
		cookie                    = "no"
		token                     = "no"
		err                       error
	)

	// 执行
	num1 := 0
	for {
		//time.Sleep(5 * time.Second)
		time.Sleep(28 * time.Millisecond)
		start := time.Now()

		// 重新初始化数据
		if 0 < len(binancePositionMap) {
			binancePositionMapCompare = make(map[string]*entity.TraderPosition, 0)
			for k, vBinancePositionMap := range binancePositionMap {
				binancePositionMapCompare[k] = vBinancePositionMap
			}
		}

		if "no" == cookie || "no" == token {
			// 数据库必须信息
			err = g.Model("zy_trader_cookie").Ctx(ctx).Where("trader_id=? and is_open=?", 1, 1).
				OrderDesc("update_time").Limit(1).Scan(&zyTraderCookie)
			if nil != err {
				//fmt.Println("龟兔，cookie，数据库查询错误：", err)
				time.Sleep(time.Second * 3)
				continue
			}

			if 0 >= len(zyTraderCookie) || 0 >= len(zyTraderCookie[0].Cookie) || 0 >= len(zyTraderCookie[0].Token) {
				//fmt.Println("龟兔，cookie，无可用：", err)
				time.Sleep(time.Second * 3)
				continue
			}

			// 更新
			cookie = zyTraderCookie[0].Cookie
			token = zyTraderCookie[0].Token
		}

		// 执行
		var (
			retry           = false
			retryTimes      = 0
			retryTimesLimit = 5 // 重试次数
			cookieErr       = false
		)

		for retryTimes < retryTimesLimit { // 最大重试
			// 龟兔的数据
			reqResData, retry, err = s.requestBinancePositionHistoryNew(traderNum, cookie, token)
			//reqResData, retry, err = s.requestProxyBinancePositionHistoryNew("http://43.130.227.135:888/", traderNum, cookie, token)
			num1++
			if 0 == num1%20000 {
				fmt.Println(time.Now(), len(reqResData), baseMoneyGuiTu.Val())
			}

			// 需要重试
			if retry {
				retryTimes++
				time.Sleep(time.Second * 5)
				fmt.Println("龟兔，重试：", retry)
				continue
			}

			// cookie不好使
			if 0 >= len(reqResData) {
				retryTimes++
				cookieErr = true
				continue
			} else {
				cookieErr = false
				break
			}
		}

		// 记录时间
		timePull := time.Since(start)

		// cookie 错误
		if cookieErr {
			cookie = "no"
			token = "no"

			fmt.Println("龟兔，cookie错误，信息", traderNum, reqResData)
			err = g.DB().Transaction(context.TODO(), func(ctx context.Context, tx gdb.TX) error {
				zyTraderCookie[0].IsOpen = 0
				_, err = tx.Ctx(ctx).Update("zy_trader_cookie", zyTraderCookie[0], "id", zyTraderCookie[0].Id)
				if nil != err {
					fmt.Println("龟兔，cookie错误，信息", traderNum, reqResData)
					return err
				}

				return nil
			})
			if nil != err {
				fmt.Println("龟兔，cookie错误，更新数据库错误，信息", traderNum, err)
			}

			continue
		}

		// 用于数据库更新
		insertData := make([]*do.TraderPosition, 0)
		updateData := make([]*do.TraderPosition, 0)
		// 用于下单
		orderInsertData := make([]*do.TraderPosition, 0)
		orderUpdateData := make([]*do.TraderPosition, 0)
		for _, vReqResData := range reqResData {
			// 新增
			var (
				currentAmount    float64
				currentAmountAbs float64
			)
			currentAmount, err = strconv.ParseFloat(vReqResData.PositionAmount, 64)
			if nil != err {
				fmt.Println("新，解析金额出错，信息", vReqResData, currentAmount, traderNum)
			}
			currentAmountAbs = math.Abs(currentAmount) // 绝对值

			if _, ok := binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide]; !ok {
				if "BOTH" != vReqResData.PositionSide { // 单项持仓
					// 加入数据库
					insertData = append(insertData, &do.TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
					})

					// 下单
					if IsEqual(currentAmountAbs, 0) {
						continue
					}

					orderInsertData = append(orderInsertData, &do.TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
					})
				} else {
					// 加入数据库
					insertData = append(insertData, &do.TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmount, // 正负数保持
					})

					// 模拟为多空仓，下单，todo 组合式的判断应该时牢靠的
					var tmpPositionSide string
					if IsEqual(currentAmount, 0) {
						continue
					} else if math.Signbit(currentAmount) {
						// 模拟空
						tmpPositionSide = "SHORT"
						orderInsertData = append(orderInsertData, &do.TraderPosition{
							Symbol:         vReqResData.Symbol,
							PositionSide:   tmpPositionSide,
							PositionAmount: currentAmountAbs, // 变成绝对值
						})
					} else {
						// 模拟多
						tmpPositionSide = "LONG"
						orderInsertData = append(orderInsertData, &do.TraderPosition{
							Symbol:         vReqResData.Symbol,
							PositionSide:   tmpPositionSide,
							PositionAmount: currentAmountAbs, // 变成绝对值
						})
					}
				}
			} else {
				// 数量无变化
				if "BOTH" != vReqResData.PositionSide {
					if IsEqual(currentAmountAbs, binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
						continue
					}

					updateData = append(updateData, &do.TraderPosition{
						Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
					})

					orderUpdateData = append(orderUpdateData, &do.TraderPosition{
						Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
					})
				} else {
					if IsEqual(currentAmount, binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
						continue
					}

					updateData = append(updateData, &do.TraderPosition{
						Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmount, // 正负数保持
					})

					// 第一步：构造虚拟的上一次仓位，空或多或无
					// 这里修改一下历史仓位的信息，方便程序在后续的流程中使用，模拟both的positionAmount为正数时，修改仓位对应的多仓方向的数据，为负数时修改空仓位的数据，0时不处理
					if _, ok = binancePositionMap[vReqResData.Symbol+"SHORT"]; !ok {
						fmt.Println("新，缺少仓位SHORT，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
						continue
					}
					if _, ok = binancePositionMap[vReqResData.Symbol+"LONG"]; !ok {
						fmt.Println("新，缺少仓位LONG，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
						continue
					}

					var lastPositionSide string // 上次仓位
					binancePositionMapCompare[vReqResData.Symbol+"SHORT"] = &entity.TraderPosition{
						Id:             binancePositionMapCompare[vReqResData.Symbol+"SHORT"].Id,
						Symbol:         binancePositionMapCompare[vReqResData.Symbol+"SHORT"].Symbol,
						PositionSide:   binancePositionMapCompare[vReqResData.Symbol+"SHORT"].PositionSide,
						PositionAmount: 0,
						CreatedAt:      binancePositionMapCompare[vReqResData.Symbol+"SHORT"].CreatedAt,
						UpdatedAt:      binancePositionMapCompare[vReqResData.Symbol+"SHORT"].UpdatedAt,
					}
					binancePositionMapCompare[vReqResData.Symbol+"LONG"] = &entity.TraderPosition{
						Id:             binancePositionMapCompare[vReqResData.Symbol+"LONG"].Id,
						Symbol:         binancePositionMapCompare[vReqResData.Symbol+"LONG"].Symbol,
						PositionSide:   binancePositionMapCompare[vReqResData.Symbol+"LONG"].PositionSide,
						PositionAmount: 0,
						CreatedAt:      binancePositionMapCompare[vReqResData.Symbol+"LONG"].CreatedAt,
						UpdatedAt:      binancePositionMapCompare[vReqResData.Symbol+"LONG"].UpdatedAt,
					}

					if IsEqual(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount, 0) { // both仓为0
						// 认为两仓都无

					} else if math.Signbit(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
						lastPositionSide = "SHORT"
						binancePositionMapCompare[vReqResData.Symbol+"SHORT"].PositionAmount = math.Abs(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount)
					} else {
						lastPositionSide = "LONG"
						binancePositionMapCompare[vReqResData.Symbol+"LONG"].PositionAmount = math.Abs(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount)
					}

					// 本次仓位
					var tmpPositionSide string
					if IsEqual(currentAmount, 0) { // 本次仓位是0
						if 0 >= len(lastPositionSide) {
							// 本次和上一次仓位都是0，应该不会走到这里
							fmt.Println("新，仓位异常逻辑，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
							continue
						}

						// 仍为是一次完全平仓，仓位和上一次保持一致
						tmpPositionSide = lastPositionSide
					} else if math.Signbit(currentAmount) { // 判断有无符号
						// 第二步：本次仓位

						// 上次和本次相反需要平上次
						if "LONG" == lastPositionSide {
							orderUpdateData = append(orderUpdateData, &do.TraderPosition{
								Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
								Symbol:         vReqResData.Symbol,
								PositionSide:   lastPositionSide,
								PositionAmount: float64(0),
							})
						}

						tmpPositionSide = "SHORT"
					} else {
						// 第二步：本次仓位

						// 上次和本次相反需要平上次
						if "SHORT" == lastPositionSide {
							orderUpdateData = append(orderUpdateData, &do.TraderPosition{
								Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
								Symbol:         vReqResData.Symbol,
								PositionSide:   lastPositionSide,
								PositionAmount: float64(0),
							})
						}

						tmpPositionSide = "LONG"
					}

					orderUpdateData = append(orderUpdateData, &do.TraderPosition{
						Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
						Symbol:         vReqResData.Symbol,
						PositionSide:   tmpPositionSide,
						PositionAmount: currentAmountAbs,
					})
				}
			}
		}

		if 0 >= len(insertData) && 0 >= len(updateData) {
			continue
		}

		// 新增数据
		tmpIdCurrent := len(binancePositionMap) + 1
		for _, vIBinancePosition := range insertData {
			binancePositionMap[vIBinancePosition.Symbol.(string)+vIBinancePosition.PositionSide.(string)] = &entity.TraderPosition{
				Id:             uint(tmpIdCurrent),
				Symbol:         vIBinancePosition.Symbol.(string),
				PositionSide:   vIBinancePosition.PositionSide.(string),
				PositionAmount: vIBinancePosition.PositionAmount.(float64),
			}
		}

		// 更新仓位数据
		for _, vUBinancePosition := range updateData {
			binancePositionMap[vUBinancePosition.Symbol.(string)+vUBinancePosition.PositionSide.(string)] = &entity.TraderPosition{
				Id:             vUBinancePosition.Id.(uint),
				Symbol:         vUBinancePosition.Symbol.(string),
				PositionSide:   vUBinancePosition.PositionSide.(string),
				PositionAmount: vUBinancePosition.PositionAmount.(float64),
			}
		}

		// 推送订单，数据库已初始化仓位，新仓库
		if 0 >= len(binancePositionMapCompare) {
			fmt.Println("初始化仓位成功")
			continue
		}

		fmt.Printf("龟兔，程序拉取部分，开始 %v, 拉取时长: %v, 统计更新时长: %v\n", start, timePull, time.Since(start))

		wg := sync.WaitGroup{}
		// 遍历跟单者
		tmpTraderBaseMoney := baseMoneyGuiTu.Val()
		globalUsers.Iterator(func(k interface{}, v interface{}) bool {
			tmpUser := v.(*entity.NewUser)

			var tmpUserBindTradersAmount float64
			if !baseMoneyUserAllMap.Contains(int(tmpUser.Id)) {
				fmt.Println("龟兔，保证金不存在：", tmpUser)
				return true
			}
			tmpUserBindTradersAmount = baseMoneyUserAllMap.Get(int(tmpUser.Id)).(float64)

			if lessThanOrEqualZero(tmpUserBindTradersAmount, 0, 1e-7) {
				fmt.Println("龟兔，保证金不足为0：", tmpUserBindTradersAmount, tmpUser)
				return true
			}

			strUserId := strconv.FormatUint(uint64(tmpUser.Id), 10)
			if 0 >= len(tmpUser.ApiSecret) || 0 >= len(tmpUser.ApiKey) {
				fmt.Println("龟兔，用户的信息无效了，信息", traderNum, tmpUser)
				return true
			}

			if lessThanOrEqualZero(tmpTraderBaseMoney, 0, 1e-7) {
				fmt.Println("龟兔，交易员信息无效了，信息", tmpUser)
				return true
			}

			// 新增仓位
			for _, vInsertData := range orderInsertData {
				// 一个新symbol通常3个开仓方向short，long，both，屏蔽一下未真实开仓的
				tmpInsertData := vInsertData
				if lessThanOrEqualZero(tmpInsertData.PositionAmount.(float64), 0, 1e-7) {
					continue
				}

				if !symbolsMap.Contains(tmpInsertData.Symbol.(string)) {
					fmt.Println("龟兔，代币信息无效，信息", tmpInsertData, tmpUser)
					continue
				}

				var (
					tmpQty        float64
					quantity      string
					quantityFloat float64
					side          string
					positionSide  string
					orderType     = "MARKET"
				)
				if "LONG" == tmpInsertData.PositionSide {
					positionSide = "LONG"
					side = "BUY"
				} else if "SHORT" == tmpInsertData.PositionSide {
					positionSide = "SHORT"
					side = "SELL"
				} else {
					fmt.Println("龟兔，无效信息，信息", vInsertData)
					continue
				}

				// 本次 代单员币的数量 * (用户保证金/代单员保证金)
				tmpQty = tmpInsertData.PositionAmount.(float64) * tmpUserBindTradersAmount / tmpTraderBaseMoney // 本次开单数量

				// 精度调整
				if 0 >= symbolsMap.Get(tmpInsertData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision {
					quantity = fmt.Sprintf("%d", int64(tmpQty))
				} else {
					quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(tmpInsertData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision, 64)
				}

				quantityFloat, err = strconv.ParseFloat(quantity, 64)
				if nil != err {
					fmt.Println(err)
					continue
				}

				if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
					continue
				}

				wg.Add(1)
				err = s.pool.Add(ctx, func(ctx context.Context) {
					defer wg.Done()

					// 下单，不用计算数量，新仓位
					// 新订单数据
					currentOrder := &entity.NewUserOrderTwo{
						UserId:        tmpUser.Id,
						TraderId:      1,
						Symbol:        tmpInsertData.Symbol.(string),
						Side:          side,
						PositionSide:  positionSide,
						Quantity:      quantityFloat,
						Price:         0,
						TraderQty:     tmpInsertData.PositionAmount.(float64),
						OrderType:     orderType,
						ClosePosition: "",
						CumQuote:      0,
						ExecutedQty:   0,
						AvgPrice:      0,
					}

					var (
						binanceOrderRes *binanceOrder
						orderInfoRes    *orderInfo
					)
					// 请求下单
					binanceOrderRes, orderInfoRes, err = requestBinanceOrder(tmpInsertData.Symbol.(string), side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
					if nil != err {
						fmt.Println(err)
					}

					//binanceOrderRes = &binanceOrder{
					//	OrderId:       1,
					//	ExecutedQty:   quantity,
					//	ClientOrderId: "",
					//	Symbol:        "",
					//	AvgPrice:      "",
					//	CumQuote:      "",
					//	Side:          "",
					//	PositionSide:  "",
					//	ClosePosition: false,
					//	Type:          "",
					//	Status:        "",
					//}

					// 下单异常
					if 0 >= binanceOrderRes.OrderId {
						orderErr.Add(&entity.NewUserOrderErrTwo{
							UserId:        currentOrder.UserId,
							TraderId:      currentOrder.TraderId,
							ClientOrderId: "",
							OrderId:       "",
							Symbol:        currentOrder.Symbol,
							Side:          currentOrder.Side,
							PositionSide:  currentOrder.PositionSide,
							Quantity:      quantityFloat,
							Price:         currentOrder.Price,
							TraderQty:     currentOrder.TraderQty,
							OrderType:     currentOrder.OrderType,
							ClosePosition: currentOrder.ClosePosition,
							CumQuote:      currentOrder.CumQuote,
							ExecutedQty:   currentOrder.ExecutedQty,
							AvgPrice:      currentOrder.AvgPrice,
							HandleStatus:  currentOrder.HandleStatus,
							Code:          int(orderInfoRes.Code),
							Msg:           orderInfoRes.Msg,
							Proportion:    0,
						})

						return
					}

					var tmpExecutedQty float64
					tmpExecutedQty, err = strconv.ParseFloat(binanceOrderRes.ExecutedQty, 64)
					if nil != err {
						fmt.Println("龟兔，下单错误，解析错误2，信息", err, currentOrder, binanceOrderRes)
						return
					}

					// 不存在新增，这里只能是开仓
					if !orderMap.Contains(tmpInsertData.Symbol.(string) + "&" + positionSide + "&" + strUserId) {
						orderMap.Set(tmpInsertData.Symbol.(string)+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
					} else {
						tmpExecutedQty += orderMap.Get(tmpInsertData.Symbol.(string) + "&" + positionSide + "&" + strUserId).(float64)
						orderMap.Set(tmpInsertData.Symbol.(string)+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
					}

					return
				})
				if nil != err {
					fmt.Println("龟兔，添加下单任务异常，新增仓位，错误信息：", err, traderNum, vInsertData, tmpUser)
				}
			}

			// 修改仓位
			for _, vUpdateData := range orderUpdateData {
				tmpUpdateData := vUpdateData
				if _, ok := binancePositionMapCompare[vUpdateData.Symbol.(string)+vUpdateData.PositionSide.(string)]; !ok {
					fmt.Println("龟兔，添加下单任务异常，修改仓位，错误信息：", err, traderNum, vUpdateData, tmpUser)
					continue
				}
				lastPositionData := binancePositionMapCompare[vUpdateData.Symbol.(string)+vUpdateData.PositionSide.(string)]

				if !symbolsMap.Contains(tmpUpdateData.Symbol.(string)) {
					fmt.Println("龟兔，代币信息无效，信息", tmpUpdateData, tmpUser)
					continue
				}

				var (
					tmpQty        float64
					quantity      string
					quantityFloat float64
					side          string
					positionSide  string
					orderType     = "MARKET"
				)

				if lessThanOrEqualZero(tmpUpdateData.PositionAmount.(float64), 0, 1e-7) {
					fmt.Println("龟兔，完全平仓：", tmpUpdateData)
					// 全平仓
					if "LONG" == tmpUpdateData.PositionSide {
						positionSide = "LONG"
						side = "SELL"
					} else if "SHORT" == tmpUpdateData.PositionSide {
						positionSide = "SHORT"
						side = "BUY"
					} else {
						fmt.Println("龟兔，无效信息，信息", tmpUpdateData)
						continue
					}

					// 未开启过仓位
					if !orderMap.Contains(tmpUpdateData.Symbol.(string) + "&" + tmpUpdateData.PositionSide.(string) + "&" + strUserId) {
						continue
					}

					// 认为是0
					if lessThanOrEqualZero(orderMap.Get(tmpUpdateData.Symbol.(string)+"&"+tmpUpdateData.PositionSide.(string)+"&"+strUserId).(float64), 0, 1e-7) {
						continue
					}

					// 剩余仓位
					tmpQty = orderMap.Get(tmpUpdateData.Symbol.(string) + "&" + tmpUpdateData.PositionSide.(string) + "&" + strUserId).(float64)
				} else if lessThanOrEqualZero(lastPositionData.PositionAmount, tmpUpdateData.PositionAmount.(float64), 1e-7) {
					fmt.Println("龟兔，追加仓位：", tmpUpdateData, lastPositionData)
					// 本次加仓 代单员币的数量 * (用户保证金/代单员保证金)
					if "LONG" == tmpUpdateData.PositionSide {
						positionSide = "LONG"
						side = "BUY"
					} else if "SHORT" == tmpUpdateData.PositionSide {
						positionSide = "SHORT"
						side = "SELL"
					} else {
						fmt.Println("龟兔，无效信息，信息", tmpUpdateData)
						continue
					}

					// 本次减去上一次
					tmpQty = (tmpUpdateData.PositionAmount.(float64) - lastPositionData.PositionAmount) * tmpUserBindTradersAmount / tmpTraderBaseMoney // 本次开单数量
				} else if lessThanOrEqualZero(tmpUpdateData.PositionAmount.(float64), lastPositionData.PositionAmount, 1e-7) {
					fmt.Println("龟兔，部分平仓：", tmpUpdateData, lastPositionData)
					// 部分平仓
					if "LONG" == tmpUpdateData.PositionSide {
						positionSide = "LONG"
						side = "SELL"
					} else if "SHORT" == tmpUpdateData.PositionSide {
						positionSide = "SHORT"
						side = "BUY"
					} else {
						fmt.Println("龟兔，无效信息，信息", tmpUpdateData)
						continue
					}

					// 未开启过仓位
					if !orderMap.Contains(tmpUpdateData.Symbol.(string) + "&" + tmpUpdateData.PositionSide.(string) + "&" + strUserId) {
						continue
					}

					// 认为是0
					if lessThanOrEqualZero(orderMap.Get(tmpUpdateData.Symbol.(string)+"&"+tmpUpdateData.PositionSide.(string)+"&"+strUserId).(float64), 0, 1e-7) {
						continue
					}

					// 上次仓位
					if lessThanOrEqualZero(lastPositionData.PositionAmount, 0, 1e-7) {
						fmt.Println("龟兔，部分平仓，上次仓位信息无效，信息", lastPositionData, tmpUpdateData)
						continue
					}

					// 按百分比
					tmpQty = orderMap.Get(tmpUpdateData.Symbol.(string)+"&"+tmpUpdateData.PositionSide.(string)+"&"+strUserId).(float64) * (lastPositionData.PositionAmount - tmpUpdateData.PositionAmount.(float64)) / lastPositionData.PositionAmount
				} else {
					fmt.Println("龟兔，分析仓位无效，信息", lastPositionData, tmpUpdateData)
					continue
				}

				// 精度调整
				if 0 >= symbolsMap.Get(tmpUpdateData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision {
					quantity = fmt.Sprintf("%d", int64(tmpQty))
				} else {
					quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(tmpUpdateData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision, 64)
				}

				quantityFloat, err = strconv.ParseFloat(quantity, 64)
				if nil != err {
					fmt.Println(err)
					continue
				}

				if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
					continue
				}

				wg.Add(1)
				err = s.pool.Add(ctx, func(ctx context.Context) {
					defer wg.Done()

					// 下单，不用计算数量，新仓位
					// 新订单数据
					currentOrder := &entity.NewUserOrderTwo{
						UserId:        tmpUser.Id,
						TraderId:      1,
						Symbol:        tmpUpdateData.Symbol.(string),
						Side:          side,
						PositionSide:  positionSide,
						Quantity:      quantityFloat,
						Price:         0,
						TraderQty:     quantityFloat,
						OrderType:     orderType,
						ClosePosition: "",
						CumQuote:      0,
						ExecutedQty:   0,
						AvgPrice:      0,
					}

					var (
						binanceOrderRes *binanceOrder
						orderInfoRes    *orderInfo
					)
					// 请求下单
					binanceOrderRes, orderInfoRes, err = requestBinanceOrder(tmpUpdateData.Symbol.(string), side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
					if nil != err {
						fmt.Println(err)
						return
					}

					//binanceOrderRes = &binanceOrder{
					//	OrderId:       1,
					//	ExecutedQty:   quantity,
					//	ClientOrderId: "",
					//	Symbol:        "",
					//	AvgPrice:      "",
					//	CumQuote:      "",
					//	Side:          "",
					//	PositionSide:  "",
					//	ClosePosition: false,
					//	Type:          "",
					//	Status:        "",
					//}

					// 下单异常
					if 0 >= binanceOrderRes.OrderId {
						// 写入
						orderErr.Add(&entity.NewUserOrderErrTwo{
							UserId:        currentOrder.UserId,
							TraderId:      currentOrder.TraderId,
							ClientOrderId: "",
							OrderId:       "",
							Symbol:        currentOrder.Symbol,
							Side:          currentOrder.Side,
							PositionSide:  currentOrder.PositionSide,
							Quantity:      quantityFloat,
							Price:         currentOrder.Price,
							TraderQty:     currentOrder.TraderQty,
							OrderType:     currentOrder.OrderType,
							ClosePosition: currentOrder.ClosePosition,
							CumQuote:      currentOrder.CumQuote,
							ExecutedQty:   currentOrder.ExecutedQty,
							AvgPrice:      currentOrder.AvgPrice,
							HandleStatus:  currentOrder.HandleStatus,
							Code:          int(orderInfoRes.Code),
							Msg:           orderInfoRes.Msg,
							Proportion:    0,
						})

						return // 返回
					}

					var tmpExecutedQty float64
					tmpExecutedQty, err = strconv.ParseFloat(binanceOrderRes.ExecutedQty, 64)
					if nil != err {
						fmt.Println("龟兔，下单错误，解析错误2，信息", err, currentOrder, binanceOrderRes)
						return
					}

					// 不存在新增，这里只能是开仓
					if !orderMap.Contains(tmpUpdateData.Symbol.(string) + "&" + positionSide + "&" + strUserId) {
						// 追加仓位，开仓
						if "LONG" == positionSide && "BUY" == side {
							orderMap.Set(tmpUpdateData.Symbol.(string)+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
						} else if "SHORT" == positionSide && "SELL" == side {
							orderMap.Set(tmpUpdateData.Symbol.(string)+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
						} else {
							fmt.Println("龟兔，未知仓位信息，信息", tmpUpdateData, tmpExecutedQty)
						}

					} else {
						// 追加仓位，开仓
						if "LONG" == positionSide {
							if "BUY" == side {
								tmpExecutedQty += orderMap.Get(tmpUpdateData.Symbol.(string) + "&" + positionSide + "&" + strUserId).(float64)
								orderMap.Set(tmpUpdateData.Symbol.(string)+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
							} else if "SELL" == side {
								tmpExecutedQty = orderMap.Get(tmpUpdateData.Symbol.(string)+"&"+positionSide+"&"+strUserId).(float64) - tmpExecutedQty
								if lessThanOrEqualZero(tmpExecutedQty, 0, 1e-7) {
									tmpExecutedQty = 0
								}
								orderMap.Set(tmpUpdateData.Symbol.(string)+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
							} else {
								fmt.Println("龟兔，未知仓位信息，信息", tmpUpdateData, tmpExecutedQty)
							}

						} else if "SHORT" == positionSide {
							if "SELL" == side {
								tmpExecutedQty += orderMap.Get(tmpUpdateData.Symbol.(string) + "&" + positionSide + "&" + strUserId).(float64)
								orderMap.Set(tmpUpdateData.Symbol.(string)+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
							} else if "BUY" == side {
								tmpExecutedQty = orderMap.Get(tmpUpdateData.Symbol.(string)+"&"+positionSide+"&"+strUserId).(float64) - tmpExecutedQty
								if lessThanOrEqualZero(tmpExecutedQty, 0, 1e-7) {
									tmpExecutedQty = 0
								}
								orderMap.Set(tmpUpdateData.Symbol.(string)+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
							} else {
								fmt.Println("龟兔，未知仓位信息，信息", tmpUpdateData, tmpExecutedQty)
							}

						} else {
							fmt.Println("龟兔，未知仓位信息，信息", tmpUpdateData, tmpExecutedQty)
						}
					}

					return
				})
				if nil != err {
					fmt.Println("新，添加下单任务异常，修改仓位，错误信息：", err, traderNum, vUpdateData, tmpUser)
				}
			}

			return true
		})

		// 回收协程
		wg.Wait()

		fmt.Printf("龟兔，程序执行完毕，开始 %v, 拉取时长: %v, 总计时长: %v\n", start, timePull, time.Since(start))
	}
}

type lastOrderStruct struct {
	orderId   string
	orderInfo *binanceOrder
	user      *entity.NewUser
	time      time.Time
}

type lastStruct struct {
	lock bool
	time time.Time
}

var (
	last        = gmap.New(true) // 初始化下单记录
	lastOrders  = gmap.New(true) // 止盈单信息
	lastOrders2 = gmap.New(true) // 止损单信息
)

// PullAndOrderNewGuiTuPlay 拉取binance数据，新玩法滑点模式，仓位，根据cookie 龟兔赛跑
func (s *sBinanceTraderHistory) PullAndOrderNewGuiTuPlay(ctx context.Context) {
	var (
		traderNum            = globalTraderNum // 龟兔
		zyTraderCookie       []*entity.ZyTraderCookie
		cookie               = "no"
		token                = "no"
		exchangeInfo         []*BinanceExchangeInfoSymbol
		exchangeInfoTickSize map[string]float64
		err                  error
	)

	exchangeInfo, err = requestBinanceExchangeInfo()
	if nil == exchangeInfo || 0 >= len(exchangeInfo) {
		fmt.Println("初始化错误，查询币安交易对信息错误")
		return
	}

	exchangeInfoTickSize = make(map[string]float64, 0)
	for _, vExchangeInfo := range exchangeInfo {
		for _, vFilter := range vExchangeInfo.Filters {
			if "PRICE_FILTER" == vFilter.FilterType {
				exchangeInfoTickSize[vExchangeInfo.Symbol], err = strconv.ParseFloat(vFilter.TickSize, 64)
				if nil != err {
					fmt.Println("初始化错误，查询币安交易对信息错误，解析错误", vFilter, vExchangeInfo.Symbol)
				}
			}
		}
	}

	//last := time.Now() // 初始化 last 为当前时间
	//var (
	//	xTime = 2 * time.Second
	//)

	exMap := make(map[string]bool, 0)
	exMap["ETHUSDT"] = true
	exMap["BTCUSDT"] = true
	exMap["1000PEPEUSDT"] = true
	exMap["SOLUSDT"] = true
	exMap["FILUSDT"] = true
	exMap["DOGEUSDT"] = true
	exMap["1000SHIBIUSDT"] = true
	exMap["BNBUSDT"] = true
	exMap["LTCUSDT"] = true
	exMap["XRPUSDT"] = true

	// 执行
	for {
		//time.Sleep(5 * time.Second)
		time.Sleep(28 * time.Millisecond)
		start := time.Now()

		var (
			reqResData                []*binancePositionDataList
			binancePositionMapCompare map[string]*entity.TraderPosition
		)
		// 重新初始化数据
		if 0 < len(binancePositionMap) {
			binancePositionMapCompare = make(map[string]*entity.TraderPosition, 0)
			for k, vBinancePositionMap := range binancePositionMap {
				binancePositionMapCompare[k] = vBinancePositionMap
			}
		}

		if "no" == cookie || "no" == token {
			// 数据库必须信息
			err = g.Model("zy_trader_cookie").Ctx(ctx).Where("trader_id=? and is_open=?", 1, 1).
				OrderDesc("update_time").Limit(1).Scan(&zyTraderCookie)
			if nil != err {
				//fmt.Println("龟兔，cookie，数据库查询错误：", err)
				time.Sleep(time.Second * 3)
				continue
			}

			if 0 >= len(zyTraderCookie) || 0 >= len(zyTraderCookie[0].Cookie) || 0 >= len(zyTraderCookie[0].Token) {
				//fmt.Println("龟兔，cookie，无可用：", err)
				time.Sleep(time.Second * 3)
				continue
			}

			// 更新
			cookie = zyTraderCookie[0].Cookie
			token = zyTraderCookie[0].Token
		}

		// 执行
		var (
			retry           = false
			retryTimes      = 0
			retryTimesLimit = 5 // 重试次数
			cookieErr       = false
		)

		for retryTimes < retryTimesLimit { // 最大重试
			// 龟兔的数据
			reqResData, retry, err = s.requestBinancePositionHistoryNew(traderNum, cookie, token)
			//reqResData, retry, err = s.requestProxyBinancePositionHistoryNew("http://43.130.227.135:888/", traderNum, cookie, token)

			// 需要重试
			if retry {
				retryTimes++
				time.Sleep(time.Second * 5)
				log.Println("龟兔，重试：", retry)
				continue
			}

			// cookie不好使
			if 0 >= len(reqResData) {
				retryTimes++
				cookieErr = true
				continue
			} else {
				cookieErr = false
				break
			}
		}

		// 记录时间
		timePull := time.Since(start)

		// cookie 错误
		if cookieErr {
			cookie = "no"
			token = "no"

			log.Println("龟兔，cookie错误，信息", traderNum, reqResData)
			err = g.DB().Transaction(context.TODO(), func(ctx context.Context, tx gdb.TX) error {
				zyTraderCookie[0].IsOpen = 0
				_, err = tx.Ctx(ctx).Update("zy_trader_cookie", zyTraderCookie[0], "id", zyTraderCookie[0].Id)
				if nil != err {
					log.Println("龟兔，cookie错误，信息", traderNum, reqResData)
					return err
				}

				return nil
			})
			if nil != err {
				log.Println("龟兔，cookie错误，更新数据库错误，信息", traderNum, err)
			}

			continue
		}

		// 用于数据库更新
		insertData := make([]*do.TraderPosition, 0)
		updateData := make([]*do.TraderPosition, 0)
		// 用于下单
		orderInsertData := make([]*do.TraderPosition, 0)
		orderUpdateData := make([]*do.TraderPosition, 0)
		for _, vReqResData := range reqResData {
			// 新增
			var (
				currentAmount    float64
				currentAmountAbs float64
				markPrice        float64
			)
			currentAmount, err = strconv.ParseFloat(vReqResData.PositionAmount, 64)
			if nil != err {
				log.Println("新，解析金额出错，信息", vReqResData, currentAmount, traderNum)
			}
			currentAmountAbs = math.Abs(currentAmount) // 绝对值

			markPrice, err = strconv.ParseFloat(vReqResData.MarkPrice, 64)
			if nil != err {
				log.Println("新，解析价格出错，信息", vReqResData, markPrice, traderNum)
			}

			if _, ok := binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide]; !ok {
				if "BOTH" != vReqResData.PositionSide { // 单项持仓
					// 加入数据库
					insertData = append(insertData, &do.TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
						MarkPrice:      markPrice,
					})

					// 下单
					if IsEqual(currentAmountAbs, 0) {
						continue
					}

					orderInsertData = append(orderInsertData, &do.TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
						MarkPrice:      markPrice,
					})
				} else {

					// 加入数据库
					insertData = append(insertData, &do.TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmount, // 正负数保持
						MarkPrice:      markPrice,
					})

					// 模拟为多空仓，下单，todo 组合式的判断应该时牢靠的
					var tmpPositionSide string
					if IsEqual(currentAmount, 0) {
						continue
					} else if math.Signbit(currentAmount) {
						// 模拟空
						tmpPositionSide = "SHORT"
						orderInsertData = append(orderInsertData, &do.TraderPosition{
							Symbol:         vReqResData.Symbol,
							PositionSide:   tmpPositionSide,
							PositionAmount: currentAmountAbs, // 变成绝对值
							MarkPrice:      markPrice,
						})
					} else {
						// 模拟多
						tmpPositionSide = "LONG"
						orderInsertData = append(orderInsertData, &do.TraderPosition{
							Symbol:         vReqResData.Symbol,
							PositionSide:   tmpPositionSide,
							PositionAmount: currentAmountAbs, // 变成绝对值
							MarkPrice:      markPrice,
						})
					}
				}
			} else {
				// 数量无变化
				if "BOTH" != vReqResData.PositionSide {
					if IsEqual(currentAmountAbs, binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
						continue
					}

					updateData = append(updateData, &do.TraderPosition{
						Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
						MarkPrice:      markPrice,
					})

					orderUpdateData = append(orderUpdateData, &do.TraderPosition{
						Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
						MarkPrice:      markPrice,
					})
				} else {

					if IsEqual(currentAmount, binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
						continue
					}

					updateData = append(updateData, &do.TraderPosition{
						Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmount, // 正负数保持
						MarkPrice:      markPrice,
					})

					// 第一步：构造虚拟的上一次仓位，空或多或无
					// 这里修改一下历史仓位的信息，方便程序在后续的流程中使用，模拟both的positionAmount为正数时，修改仓位对应的多仓方向的数据，为负数时修改空仓位的数据，0时不处理
					if _, ok = binancePositionMap[vReqResData.Symbol+"SHORT"]; !ok {
						log.Println("新，缺少仓位SHORT，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
						continue
					}
					if _, ok = binancePositionMap[vReqResData.Symbol+"LONG"]; !ok {
						log.Println("新，缺少仓位LONG，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
						continue
					}

					var lastPositionSide string // 上次仓位
					binancePositionMapCompare[vReqResData.Symbol+"SHORT"] = &entity.TraderPosition{
						Id:             binancePositionMapCompare[vReqResData.Symbol+"SHORT"].Id,
						Symbol:         binancePositionMapCompare[vReqResData.Symbol+"SHORT"].Symbol,
						PositionSide:   binancePositionMapCompare[vReqResData.Symbol+"SHORT"].PositionSide,
						PositionAmount: 0,
						CreatedAt:      binancePositionMapCompare[vReqResData.Symbol+"SHORT"].CreatedAt,
						UpdatedAt:      binancePositionMapCompare[vReqResData.Symbol+"SHORT"].UpdatedAt,
						MarkPrice:      markPrice,
					}
					binancePositionMapCompare[vReqResData.Symbol+"LONG"] = &entity.TraderPosition{
						Id:             binancePositionMapCompare[vReqResData.Symbol+"LONG"].Id,
						Symbol:         binancePositionMapCompare[vReqResData.Symbol+"LONG"].Symbol,
						PositionSide:   binancePositionMapCompare[vReqResData.Symbol+"LONG"].PositionSide,
						PositionAmount: 0,
						CreatedAt:      binancePositionMapCompare[vReqResData.Symbol+"LONG"].CreatedAt,
						UpdatedAt:      binancePositionMapCompare[vReqResData.Symbol+"LONG"].UpdatedAt,
						MarkPrice:      markPrice,
					}

					if IsEqual(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount, 0) { // both仓为0
						// 认为两仓都无

					} else if math.Signbit(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
						lastPositionSide = "SHORT"
						binancePositionMapCompare[vReqResData.Symbol+"SHORT"].PositionAmount = math.Abs(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount)
					} else {
						lastPositionSide = "LONG"
						binancePositionMapCompare[vReqResData.Symbol+"LONG"].PositionAmount = math.Abs(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount)
					}

					// 本次仓位
					var tmpPositionSide string
					if IsEqual(currentAmount, 0) { // 本次仓位是0
						if 0 >= len(lastPositionSide) {
							// 本次和上一次仓位都是0，应该不会走到这里
							log.Println("新，仓位异常逻辑，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
							continue
						}

						// 仍为是一次完全平仓，仓位和上一次保持一致
						tmpPositionSide = lastPositionSide
					} else if math.Signbit(currentAmount) { // 判断有无符号
						// 第二步：本次仓位

						// 上次和本次相反需要平上次
						if "LONG" == lastPositionSide {
							//orderUpdateData = append(orderUpdateData, &do.TraderPosition{
							//	Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
							//	Symbol:         vReqResData.Symbol,
							//	PositionSide:   lastPositionSide,
							//	PositionAmount: float64(0),
							//	MarkPrice:      markPrice,
							//})
						}

						tmpPositionSide = "SHORT"
					} else {
						// 第二步：本次仓位

						// 上次和本次相反需要平上次
						if "SHORT" == lastPositionSide {
							//orderUpdateData = append(orderUpdateData, &do.TraderPosition{
							//	Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
							//	Symbol:         vReqResData.Symbol,
							//	PositionSide:   lastPositionSide,
							//	PositionAmount: float64(0),
							//	MarkPrice:      markPrice,
							//})
						}

						tmpPositionSide = "LONG"
					}

					orderUpdateData = append(orderUpdateData, &do.TraderPosition{
						Id:             binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].Id,
						Symbol:         vReqResData.Symbol,
						PositionSide:   tmpPositionSide,
						PositionAmount: currentAmountAbs,
						MarkPrice:      markPrice,
					})
				}
			}
		}

		if 0 >= len(insertData) && 0 >= len(updateData) {
			continue
		}

		// 新增数据
		tmpIdCurrent := len(binancePositionMap) + 1
		for _, vIBinancePosition := range insertData {
			binancePositionMap[vIBinancePosition.Symbol.(string)+vIBinancePosition.PositionSide.(string)] = &entity.TraderPosition{
				Id:             uint(tmpIdCurrent),
				Symbol:         vIBinancePosition.Symbol.(string),
				PositionSide:   vIBinancePosition.PositionSide.(string),
				PositionAmount: vIBinancePosition.PositionAmount.(float64),
				MarkPrice:      vIBinancePosition.MarkPrice.(float64),
			}
		}

		// 更新仓位数据
		for _, vUBinancePosition := range updateData {
			binancePositionMap[vUBinancePosition.Symbol.(string)+vUBinancePosition.PositionSide.(string)] = &entity.TraderPosition{
				Id:             vUBinancePosition.Id.(uint),
				Symbol:         vUBinancePosition.Symbol.(string),
				PositionSide:   vUBinancePosition.PositionSide.(string),
				PositionAmount: vUBinancePosition.PositionAmount.(float64),
				MarkPrice:      vUBinancePosition.MarkPrice.(float64),
			}
		}

		// 推送订单，数据库已初始化仓位，新仓库
		if 0 >= len(binancePositionMapCompare) {
			log.Println("初始化仓位成功")
			continue
		}

		log.Printf("龟兔，程序拉取部分，开始 %v, 拉取时长: %v, 统计更新时长: %v\n", start, timePull, time.Since(start))

		//wg := sync.WaitGroup{}
		// 遍历跟单者
		tmpTraderBaseMoney := baseMoneyGuiTu.Val()
		globalUsers.Iterator(func(k interface{}, v interface{}) bool {
			tmpUser := v.(*entity.NewUser)

			var tmpUserBindTradersAmount float64
			if !baseMoneyUserAllMap.Contains(int(tmpUser.Id)) {
				log.Println("龟兔，保证金不存在：", tmpUser)
				return true
			}
			tmpUserBindTradersAmount = baseMoneyUserAllMap.Get(int(tmpUser.Id)).(float64)
			//tmpUserBindTradersAmount = 10
			if lessThanOrEqualZero(tmpUserBindTradersAmount, 0, 1e-7) {
				log.Println("龟兔，保证金不足为0：", tmpUserBindTradersAmount, tmpUser)
				return true
			}

			if 0 >= len(tmpUser.ApiSecret) || 0 >= len(tmpUser.ApiKey) {
				log.Println("龟兔，用户的信息无效了，信息", traderNum, tmpUser)
				return true
			}

			//strUserId := strconv.FormatUint(uint64(tmpUser.Id), 10)
			// 新增仓位
			for _, vInsertData := range orderInsertData {
				// 一个新symbol通常3个开仓方向short，long，both，屏蔽一下未真实开仓的
				tmpInsertData := vInsertData

				//if "ETHUSDT" == tmpInsertData.Symbol || "BTCUSDT" == tmpInsertData.Symbol {
				//	continue
				//}

				if _, ok := exMap[tmpInsertData.Symbol.(string)]; ok {
					continue
				}

				if lessThanOrEqualZero(tmpInsertData.PositionAmount.(float64), 0, 1e-7) {
					continue
				}

				if lessThanOrEqualZero(tmpInsertData.MarkPrice.(float64), 0, 1e-7) {
					log.Println("龟兔，价格信息小于0，信息", tmpInsertData)
					continue
				}

				if tmpInsertData.PositionAmount.(float64)*tmpInsertData.MarkPrice.(float64)/tmpTraderBaseMoney < tmpUser.Second {
					log.Println("龟兔，小于操作规定比例，信息", tmpInsertData, tmpTraderBaseMoney, tmpUser.Second)
					continue
				}

				if !symbolsMap.Contains(tmpInsertData.Symbol.(string)) {
					log.Println("龟兔，代币信息无效，信息", tmpInsertData, tmpUser)
					continue
				}

				var (
					tmpQty   float64
					quantity string
					//quantityFloat float64
					side         string
					stopSide     string
					positionSide string
					orderType    = "MARKET"
				)
				if "LONG" == tmpInsertData.PositionSide {
					positionSide = "LONG"
					side = "BUY"
					stopSide = "SELL"

				} else if "SHORT" == tmpInsertData.PositionSide {
					positionSide = "SHORT"
					side = "SELL"
					stopSide = "BUY"

				} else {
					log.Println("龟兔，无效信息，信息", tmpInsertData)
					continue
				}

				// 本次 保证金*50倍/币价格
				tmpQty = tmpUserBindTradersAmount * float64(tmpUser.BinanceId) / tmpInsertData.MarkPrice.(float64) // 本次开单数量

				if 0 == tmpUser.OrderType {
					// 精度调整
					if 0 >= symbolsMap.Get(tmpInsertData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision {
						quantity = fmt.Sprintf("%d", int64(tmpQty))
					} else {
						quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(tmpInsertData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision, 64)
					}

					//quantityFloat, err = strconv.ParseFloat(quantity, 64)
					//if nil != err {
					//	log.Println(err)
					//	continue
					//}
					//
					//if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
					//	continue
					//}

					//wg.Add(1)
					err = s.pool.Add(ctx, func(ctx context.Context) {
						//defer wg.Done()

						var (
							binanceOrderRes *binanceOrder
							orderInfoRes    *orderInfo
							errA            error
						)
						// 请求下单
						binanceOrderRes, orderInfoRes, errA = requestBinanceOrder(tmpInsertData.Symbol.(string), side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						if nil != errA || binanceOrderRes.OrderId <= 0 {
							log.Println("龟兔，添加下单，信息：", errA, binanceOrderRes, orderInfoRes, tmpInsertData, side, orderType, positionSide, quantity, tmpUser.Id)
							return
						}

						tmpFirst := time.Duration(tmpUser.First)
						time.Sleep(tmpFirst * time.Millisecond)

						var (
							binanceOrderRes3 *binanceOrder
							orderInfoRes3    *orderInfo
							errC             error
						)
						// 请求下单
						binanceOrderRes3, orderInfoRes3, errC = requestBinanceOrder(tmpInsertData.Symbol.(string), stopSide, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						if nil != errC || binanceOrderRes3.OrderId <= 0 {
							log.Println("龟兔，关仓，信息：", errC, binanceOrderRes3, orderInfoRes3, tmpInsertData, stopSide, orderType, positionSide, quantity, tmpUser.Id)
							return
						}

						log.Println("新，新增仓位，完成：", err, quantity, tmpUser.Id)
						return
					})
					if nil != err {
						log.Println("龟兔，添加下单任务异常，新增仓位，错误信息：", err, tmpInsertData, tmpUser)
					}
				} else if 1 == tmpUser.OrderType {
					// 精度调整
					if 0 >= symbolsMap.Get(tmpInsertData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision {
						quantity = fmt.Sprintf("%d", int64(tmpQty))
					} else {
						quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(tmpInsertData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision, 64)
					}

					//quantityFloat, err = strconv.ParseFloat(quantity, 64)
					//if nil != err {
					//	log.Println(err)
					//	continue
					//}
					//
					//if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
					//	continue
					//}

					//if _, ok := exchangeInfoTickSize[tmpInsertData.Symbol.(string)]; !ok {
					//	log.Println("龟兔，代币信息无效2，信息", tmpInsertData, tmpUser)
					//	continue
					//}
					//
					//if 0 >= exchangeInfoTickSize[tmpInsertData.Symbol.(string)] {
					//	log.Println("龟兔，代币信息无效3，信息", tmpInsertData, tmpUser)
					//	continue
					//}

					//wg.Add(1)
					err = s.pool.Add(ctx, func(ctx context.Context) {
						//defer wg.Done()
						current := time.Now()
						log.Println("当前时间：", current, "用户：", tmpUser.Id)

						tmpSecond := 59
						if 1 <= tmpUser.First {
							tmpSecond = 57
						}

						endOfMinute := time.Date(
							current.Year(),
							current.Month(),
							current.Day(),
							current.Hour(),
							current.Minute(),
							tmpSecond, // 秒设置为59
							0,         // 纳秒设为0
							current.Location(),
						)

						diffMillis := endOfMinute.Sub(current).Milliseconds()

						var (
							binanceOrderRes *binanceOrder
							orderInfoRes    *orderInfo
							errA            error
						)
						// 请求下单
						binanceOrderRes, orderInfoRes, errA = requestBinanceOrder(tmpInsertData.Symbol.(string), side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						if nil != errA || binanceOrderRes.OrderId <= 0 {
							log.Println("龟兔，添加下单，信息：", errA, binanceOrderRes, orderInfoRes, tmpInsertData, side, orderType, positionSide, quantity, tmpUser.Id)
							return
						}

						// 止盈价格
						//var (
						//	avgPrice   float64
						//	priceFloat float64
						//	price      string // 止盈价 委托价格
						//)
						//avgPrice, errA = strconv.ParseFloat(binanceOrderRes.AvgPrice, 64)
						//if nil != errA {
						//	log.Println("下单错误，avgPrive解析失败", errA)
						//	return
						//}

						//var (
						//	binanceOrderRes2 *binanceOrder
						//	orderInfoRes2    *orderInfo
						//	err2             error
						//)
						//
						//if "LONG" == positionSide {
						//	priceFloat = avgPrice + avgPrice*tmpUser.First
						//	priceFloat = math.Round(priceFloat/exchangeInfoTickSize[tmpInsertData.Symbol.(string)]) * exchangeInfoTickSize[tmpInsertData.Symbol.(string)]
						//	price = strconv.FormatFloat(priceFloat, 'f', symbolsMap.Get(tmpInsertData.Symbol.(string)).(*entity.LhCoinSymbol).PricePrecision, 64)
						//} else {
						//	priceFloat = avgPrice - avgPrice*tmpUser.First
						//	priceFloat = math.Round(priceFloat/exchangeInfoTickSize[tmpInsertData.Symbol.(string)]) * exchangeInfoTickSize[tmpInsertData.Symbol.(string)]
						//	price = strconv.FormatFloat(priceFloat, 'f', symbolsMap.Get(tmpInsertData.Symbol.(string)).(*entity.LhCoinSymbol).PricePrecision, 64)
						//}
						//
						//binanceOrderRes2, orderInfoRes2, err2 = requestBinanceOrderStopTakeProfit(tmpInsertData.Symbol.(string), stopSide, positionSide, quantity, price, price, tmpUser.ApiKey, tmpUser.ApiSecret)
						//if nil != err2 || binanceOrderRes2.OrderId <= 0 {
						//	log.Println("龟兔，添加下单，止盈失败，信息：", err, binanceOrderRes2, orderInfoRes2, tmpInsertData, stopSide, positionSide, quantity, price, price, tmpUser.Id)
						//	log.Println("当前时间：", time.Now())
						//}

						// 过了时间立马平掉
						if 0 < diffMillis {
							if time.Now().After(endOfMinute) {
							} else {
								time.Sleep(time.Duration(diffMillis) * time.Millisecond)
							}
						}

						var (
							binanceOrderRes3 *binanceOrder
							orderInfoRes3    *orderInfo
							errC             error
						)
						// 请求下单
						binanceOrderRes3, orderInfoRes3, errC = requestBinanceOrder(tmpInsertData.Symbol.(string), stopSide, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						if nil != errC || binanceOrderRes3.OrderId <= 0 {
							log.Println("龟兔，关仓，信息：", errC, binanceOrderRes3, orderInfoRes3, tmpInsertData, stopSide, orderType, positionSide, quantity, tmpUser.Id)
						}

						//// 查询进度
						//var (
						//	orderBinanceInfo *binanceOrder
						//	errCC            error
						//)
						//orderBinanceInfo, errCC = requestBinanceOrderInfo(tmpInsertData.Symbol.(string), strconv.FormatUint(uint64(binanceOrderRes2.OrderId), 10), tmpUser.ApiKey, tmpUser.ApiSecret)
						//if nil != errCC || nil == orderBinanceInfo {
						//	fmt.Println("查询止盈单信息失败：", orderBinanceInfo, errCC, tmpInsertData.Symbol.(string), binanceOrderRes2.OrderId)
						//}
						//
						//// 未变化，撤销订单
						//if binanceOrderRes2.OrderId > 0 && "NEW" == orderBinanceInfo.Status {
						//	var (
						//		binanceOrderResR *binanceOrder
						//		orderInfoResR    *orderInfo
						//	)
						//
						//	binanceOrderResR, orderInfoResR, err2 = requestBinanceDeleteOrder(tmpInsertData.Symbol.(string), strconv.FormatUint(uint64(binanceOrderRes2.OrderId), 10), tmpUser.ApiKey, tmpUser.ApiSecret)
						//	if nil != err2 || binanceOrderResR.OrderId <= 0 {
						//		log.Println("撤销止盈单，止盈，信息：", err2, binanceOrderResR, orderInfoResR, binanceOrderRes2)
						//	}
						//}

						log.Println("新，新增仓位，完成：", err, quantity, tmpUser.Id)
						return
					})
					if nil != err {
						fmt.Println("龟兔，添加下单任务异常，新增仓位，错误信息：", err, tmpInsertData, tmpUser)
					}
				} else if 2 == tmpUser.OrderType {
					if !symbolsMapGate.Contains(tmpInsertData.Symbol.(string)) {
						fmt.Println("无效gate代币信息：", err, tmpUser, tmpInsertData.Symbol.(string))
						continue
					}

					if 0 < symbolsMapGate.Get(tmpInsertData.Symbol.(string)).(*SymbolGate).QuantoMultiplier {
						//log.Println("OrderAtPlat，交易对信息错误:", user, currentData, s.SymbolsMap.Get(symbolMapKey).(*entity.LhCoinSymbol))

						// 转化为张数=币的数量/每张币的数量
						tmpQtyGate := tmpQty / symbolsMapGate.Get(tmpInsertData.Symbol.(string)).(*SymbolGate).QuantoMultiplier
						// 按张的精度转化，
						quantityInt64Gate := int64(math.Round(tmpQtyGate))
						if "LONG" == positionSide {

						} else {
							quantityInt64Gate = -quantityInt64Gate
						}

						err = s.pool.Add(ctx, func(ctx context.Context) {
							//defer wg.Done()
							current := time.Now()
							log.Println("当前时间：", current, "用户：", tmpUser.Id)

							endOfMinute := time.Date(
								current.Year(),
								current.Month(),
								current.Day(),
								current.Hour(),
								current.Minute(),
								59, // 秒设置为59
								0,  // 纳秒设为0
								current.Location(),
							)
							diffMillis := endOfMinute.Sub(current).Milliseconds()
							diffs := int32(diffMillis / 1000)
							if 0 >= diffs {
								diffs = 3
							}

							var (
								autoSize   string
								gateRes    gateapi.FuturesOrder
								symbolGate = symbolsMapGate.Get(tmpInsertData.Symbol.(string)).(*SymbolGate).Symbol
							)

							gateRes, err = placeOrderGate(tmpUser.ApiKey, tmpUser.ApiSecret, symbolGate, quantityInt64Gate, false, autoSize)
							if nil != err || 0 >= gateRes.Id {
								log.Println("OrderAtPlat，Gate下单:", gateRes, quantityInt64Gate, symbolGate)
								return
							}

							//// 止盈价格
							//var (
							//	avgPrice   float64
							//	priceFloat float64
							//	price      string // 止盈价 委托价格
							//	errAA      error
							//)
							//avgPrice, errAA = strconv.ParseFloat(gateRes.FillPrice, 64)
							//if nil != errAA {
							//	log.Println("下单错误，avgPrive解析失败，gate", errAA)
							//	return
							//}
							//
							if "LONG" == positionSide {
								autoSize = "close_long"
								//	priceFloat = avgPrice + avgPrice*tmpUser.First
								//	//priceFloat = math.Round(priceFloat/exchangeInfoTickSize[tmpInsertData.Symbol.(string)]) * exchangeInfoTickSize[tmpInsertData.Symbol.(string)]
								//	if 0 >= symbolsMapGate.Get(tmpInsertData.Symbol.(string)).(*SymbolGate).OrderPriceRound {
								//		price = fmt.Sprintf("%d", int64(priceFloat))
								//	} else {
								//		price = strconv.FormatFloat(priceFloat, 'f', symbolsMapGate.Get(tmpInsertData.Symbol.(string)).(*SymbolGate).OrderPriceRound, 64)
								//	}
								//
							} else {
								autoSize = "close_short"
								//	priceFloat = avgPrice - avgPrice*tmpUser.First
								//	if 0 >= symbolsMapGate.Get(tmpInsertData.Symbol.(string)).(*SymbolGate).OrderPriceRound {
								//		price = fmt.Sprintf("%d", int64(priceFloat))
								//	} else {
								//		price = strconv.FormatFloat(priceFloat, 'f', symbolsMapGate.Get(tmpInsertData.Symbol.(string)).(*SymbolGate).OrderPriceRound, 64)
								//	}
							}
							//
							//var (
							//	gateRes2 gateapi.FuturesOrder
							//)
							//gateRes2, err = placeLimitCloseOrderGate(tmpUser.ApiKey, tmpUser.ApiSecret, symbolGate, price, autoSize)
							//if nil != err || 0 >= gateRes2.Id {
							//	log.Println("OrderAtPlat，Gate,限价下单:", gateRes2, quantityInt64Gate, symbolGate)
							//}

							// 过了时间立马平掉
							if time.Now().After(endOfMinute) {
							} else {
								time.Sleep(time.Duration(diffMillis) * time.Millisecond)
							}

							var (
								gateRes3 gateapi.FuturesOrder
							)

							gateRes3, err = placeOrderGate(tmpUser.ApiKey, tmpUser.ApiSecret, symbolGate, 0, true, autoSize)
							if nil != err || 0 >= gateRes3.Id {
								log.Println("OrderAtPlat，Gate下单，平仓:", gateRes3, quantityInt64Gate, symbolGate)
							}

							//if gateRes2.Id > 0 {
							//	var (
							//		gateRes5 gateapi.FuturesOrder
							//	)
							//	gateRes5, err = getOrderGate(tmpUser.ApiKey, tmpUser.ApiSecret, strconv.FormatInt(gateRes2.Id, 10))
							//	if nil != err || 0 >= gateRes5.Id {
							//		log.Println("OrderAtPlat，Gate下单，查询限价:", gateRes5, quantityInt64Gate, symbolGate)
							//	}
							//	fmt.Println(gateRes5)
							//	if "open" == gateRes5.Status {
							//		var (
							//			gateRes4 gateapi.FuturesOrder
							//		)
							//		gateRes4, err = removeLimitCloseOrderGate(tmpUser.ApiKey, tmpUser.ApiSecret, strconv.FormatInt(gateRes2.Id, 10))
							//		if nil != err || 0 >= gateRes4.Id {
							//			log.Println("OrderAtPlat，Gate下单，撤销限价:", gateRes4, quantityInt64Gate, symbolGate)
							//		}
							//	}
							//}

							log.Println("新，新增仓位，完成：", err, quantityInt64Gate, tmpUser.Id)
							return
						})
						if nil != err {
							fmt.Println("龟兔，添加下单任务异常，新增仓位，错误信息：", err, tmpInsertData, tmpUser)
						}

					}
				}
			}

			// 修改仓位
			for _, vUpdateData := range orderUpdateData {
				tmpUpdateData := vUpdateData

				//if "ETHUSDT" == tmpUpdateData.Symbol || "BTCUSDT" == tmpUpdateData.Symbol {
				//	continue
				//}

				if _, ok := exMap[tmpUpdateData.Symbol.(string)]; ok {
					continue
				}

				if _, ok := binancePositionMapCompare[tmpUpdateData.Symbol.(string)+tmpUpdateData.PositionSide.(string)]; !ok {
					log.Println("龟兔，添加下单任务异常，修改仓位，错误信息：", err, traderNum, tmpUpdateData, tmpUser)
					continue
				}
				lastPositionData := binancePositionMapCompare[tmpUpdateData.Symbol.(string)+tmpUpdateData.PositionSide.(string)]

				if lessThanOrEqualZero(tmpUpdateData.MarkPrice.(float64), 0, 1e-7) {
					log.Println("龟兔，变更，价格信息小于0，信息", tmpUpdateData)
					continue
				}

				if math.Abs(lastPositionData.PositionAmount*lastPositionData.MarkPrice-tmpUpdateData.PositionAmount.(float64)*tmpUpdateData.MarkPrice.(float64))/tmpTraderBaseMoney < tmpUser.Second {
					log.Println("龟兔，变更，小于操作规定比例，信息", lastPositionData, tmpUpdateData, tmpTraderBaseMoney, tmpUser.Second)
					continue
				}

				if !symbolsMap.Contains(tmpUpdateData.Symbol.(string)) {
					log.Println("龟兔，代币信息无效，信息", tmpUpdateData, tmpUser)
					continue
				}

				var (
					tmpQty   float64
					quantity string
					//quantityFloat float64
					side         string
					stopSide     string
					positionSide string
					orderType    = "MARKET"
				)

				if lessThanOrEqualZero(tmpUpdateData.PositionAmount.(float64), 0, 1e-7) {
					log.Println("龟兔，完全平仓：", tmpUpdateData)
					// 全平仓则，开仓反向
					if "LONG" == tmpUpdateData.PositionSide {
						positionSide = "SHORT"
						side = "SELL"
						stopSide = "BUY"

					} else if "SHORT" == tmpUpdateData.PositionSide {
						positionSide = "LONG"
						side = "BUY"
						stopSide = "SELL"

					} else {
						log.Println("龟兔，无效信息，信息", tmpUpdateData)
						continue
					}

				} else if lessThanOrEqualZero(lastPositionData.PositionAmount, tmpUpdateData.PositionAmount.(float64), 1e-7) {
					log.Println("龟兔，追加仓位：", tmpUpdateData, lastPositionData)
					// 本次加仓 代单员币的数量 * (用户保证金/代单员保证金)
					if "LONG" == tmpUpdateData.PositionSide {
						positionSide = "LONG"
						side = "BUY"
						stopSide = "SELL"

					} else if "SHORT" == tmpUpdateData.PositionSide {
						positionSide = "SHORT"
						side = "SELL"
						stopSide = "BUY"

					} else {
						log.Println("龟兔，无效信息，信息", tmpUpdateData)
						continue
					}

				} else if lessThanOrEqualZero(tmpUpdateData.PositionAmount.(float64), lastPositionData.PositionAmount, 1e-7) {
					log.Println("龟兔，部分平仓：", tmpUpdateData, lastPositionData)
					// 部分平仓
					if "LONG" == tmpUpdateData.PositionSide {
						positionSide = "SHORT"
						side = "SELL"
						stopSide = "BUY"

					} else if "SHORT" == tmpUpdateData.PositionSide {
						positionSide = "LONG"
						side = "BUY"
						stopSide = "SELL"

					} else {
						log.Println("龟兔，无效信息，信息", tmpUpdateData)
						continue
					}

					// 上次仓位
					if lessThanOrEqualZero(lastPositionData.PositionAmount, 0, 1e-7) {
						log.Println("龟兔，部分平仓，上次仓位信息无效，信息", lastPositionData, tmpUpdateData)
						continue
					}

				} else {
					log.Println("龟兔，分析仓位无效，信息", lastPositionData, tmpUpdateData)
					continue
				}

				// 本次 保证金*50倍/币价格
				tmpQty = tmpUserBindTradersAmount * float64(tmpUser.BinanceId) / tmpUpdateData.MarkPrice.(float64) // 本次开单数量

				if 0 == tmpUser.OrderType {
					// 精度调整
					if 0 >= symbolsMap.Get(tmpUpdateData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision {
						quantity = fmt.Sprintf("%d", int64(tmpQty))
					} else {
						quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(tmpUpdateData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision, 64)
					}

					//quantityFloat, err = strconv.ParseFloat(quantity, 64)
					//if nil != err {
					//	log.Println(err)
					//	continue
					//}
					//
					//if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
					//	continue
					//}

					err = s.pool.Add(ctx, func(ctx context.Context) {
						//defer wg.Done()

						var (
							binanceOrderRes *binanceOrder
							orderInfoRes    *orderInfo
							errA            error
						)
						// 请求下单
						binanceOrderRes, orderInfoRes, errA = requestBinanceOrder(tmpUpdateData.Symbol.(string), side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						if nil != errA || binanceOrderRes.OrderId <= 0 {
							log.Println("龟兔，添加下单，修改仓位，信息：", errA, binanceOrderRes, orderInfoRes, tmpUpdateData, side, orderType, positionSide, quantity, tmpUser.Id)
							return
						}

						tmpFirst := time.Duration(tmpUser.First)
						time.Sleep(tmpFirst * time.Millisecond)

						var (
							binanceOrderRes3 *binanceOrder
							orderInfoRes3    *orderInfo
							errC             error
						)
						// 请求下单
						binanceOrderRes3, orderInfoRes3, errC = requestBinanceOrder(tmpUpdateData.Symbol.(string), stopSide, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						if nil != errC || binanceOrderRes3.OrderId <= 0 {
							log.Println("龟兔，关仓，信息：", errC, binanceOrderRes3, orderInfoRes3, tmpUpdateData, stopSide, orderType, positionSide, quantity, tmpUser.Id)
							return
						}

						log.Println("新，更新仓位，完成：", quantity, tmpUser.Id)
						return
					})
					if nil != err {
						log.Println("新，添加下单任务异常，修改仓位，错误信息：", err, traderNum, tmpUpdateData, tmpUser)
					}
				} else if 1 == tmpUser.OrderType {
					// 精度调整
					if 0 >= symbolsMap.Get(tmpUpdateData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision {
						quantity = fmt.Sprintf("%d", int64(tmpQty))
					} else {
						quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(tmpUpdateData.Symbol.(string)).(*entity.LhCoinSymbol).QuantityPrecision, 64)
					}

					//quantityFloat, err = strconv.ParseFloat(quantity, 64)
					//if nil != err {
					//	log.Println(err)
					//	continue
					//}
					//
					//if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
					//	continue
					//}

					//if _, ok := exchangeInfoTickSize[tmpUpdateData.Symbol.(string)]; !ok {
					//	log.Println("龟兔，代币信息无效2，信息", tmpUpdateData, tmpUser)
					//	continue
					//}
					//
					//if 0 >= exchangeInfoTickSize[tmpUpdateData.Symbol.(string)] {
					//	log.Println("龟兔，代币信息无效3，信息", tmpUpdateData, tmpUser)
					//	continue
					//}

					//wg.Add(1)
					err = s.pool.Add(ctx, func(ctx context.Context) {
						//defer wg.Done()
						current := time.Now()
						log.Println("当前时间：", current, "用户：", tmpUser.Id)

						tmpSecond := 59
						if 1 <= tmpUser.First {
							tmpSecond = 57
						}

						endOfMinute := time.Date(
							current.Year(),
							current.Month(),
							current.Day(),
							current.Hour(),
							current.Minute(),
							tmpSecond, // 秒设置为59
							0,         // 纳秒设为0
							current.Location(),
						)
						diffMillis := endOfMinute.Sub(current).Milliseconds()

						var (
							binanceOrderRes *binanceOrder
							orderInfoRes    *orderInfo
							errA            error
						)
						// 请求下单
						binanceOrderRes, orderInfoRes, errA = requestBinanceOrder(tmpUpdateData.Symbol.(string), side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						if nil != errA || binanceOrderRes.OrderId <= 0 {
							log.Println("龟兔，添加下单，信息：", errA, binanceOrderRes, orderInfoRes, tmpUpdateData, side, orderType, positionSide, quantity, tmpUser.Id)
							return
						}

						//// 止盈价格
						//var (
						//	avgPrice   float64
						//	priceFloat float64
						//	price      string // 止盈价 委托价格
						//)
						//avgPrice, errA = strconv.ParseFloat(binanceOrderRes.AvgPrice, 64)
						//if nil != errA {
						//	log.Println("下单错误，avgPrive解析失败", errA)
						//	return
						//}
						//
						//var (
						//	binanceOrderRes2 *binanceOrder
						//	orderInfoRes2    *orderInfo
						//	err2             error
						//)
						//
						//if "LONG" == positionSide {
						//	priceFloat = avgPrice + avgPrice*tmpUser.First
						//	priceFloat = math.Round(priceFloat/exchangeInfoTickSize[tmpUpdateData.Symbol.(string)]) * exchangeInfoTickSize[tmpUpdateData.Symbol.(string)]
						//	price = strconv.FormatFloat(priceFloat, 'f', symbolsMap.Get(tmpUpdateData.Symbol.(string)).(*entity.LhCoinSymbol).PricePrecision, 64)
						//} else {
						//	priceFloat = avgPrice - avgPrice*tmpUser.First
						//	priceFloat = math.Round(priceFloat/exchangeInfoTickSize[tmpUpdateData.Symbol.(string)]) * exchangeInfoTickSize[tmpUpdateData.Symbol.(string)]
						//	price = strconv.FormatFloat(priceFloat, 'f', symbolsMap.Get(tmpUpdateData.Symbol.(string)).(*entity.LhCoinSymbol).PricePrecision, 64)
						//}
						//
						//binanceOrderRes2, orderInfoRes2, err2 = requestBinanceOrderStopTakeProfit(tmpUpdateData.Symbol.(string), stopSide, positionSide, quantity, price, price, tmpUser.ApiKey, tmpUser.ApiSecret)
						//if nil != err2 || binanceOrderRes2.OrderId <= 0 {
						//	log.Println("龟兔，添加下单，止盈失败，信息：", err, binanceOrderRes2, orderInfoRes2, tmpUpdateData, stopSide, positionSide, quantity, price, price, tmpUser.Id)
						//	log.Println("当前时间：", time.Now())
						//}

						// 过了时间立马平掉
						if 0 < diffMillis {
							if time.Now().After(endOfMinute) {
							} else {
								time.Sleep(time.Duration(diffMillis) * time.Millisecond)
							}
						}

						var (
							binanceOrderRes3 *binanceOrder
							orderInfoRes3    *orderInfo
							errC             error
						)
						// 请求下单
						binanceOrderRes3, orderInfoRes3, errC = requestBinanceOrder(tmpUpdateData.Symbol.(string), stopSide, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						if nil != errC || binanceOrderRes3.OrderId <= 0 {
							log.Println("龟兔，关仓，信息：", errC, binanceOrderRes3, orderInfoRes3, tmpUpdateData, stopSide, orderType, positionSide, quantity, tmpUser.Id)
						}

						//// 查询进度
						//var (
						//	orderBinanceInfo *binanceOrder
						//	errCC            error
						//)
						//orderBinanceInfo, errCC = requestBinanceOrderInfo(tmpUpdateData.Symbol.(string), strconv.FormatUint(uint64(binanceOrderRes2.OrderId), 10), tmpUser.ApiKey, tmpUser.ApiSecret)
						//if nil != errCC || nil == orderBinanceInfo {
						//	fmt.Println("查询止盈单信息失败：", orderBinanceInfo, errCC, tmpUpdateData.Symbol.(string), binanceOrderRes2.OrderId)
						//}

						//if binanceOrderRes2.OrderId > 0 && 0 < len(orderBinanceInfo.Status) && "NEW" != orderBinanceInfo.Status {
						//	// 已经止盈
						//
						//} else {
						//	var (
						//		binanceOrderRes3 *binanceOrder
						//		orderInfoRes3    *orderInfo
						//		errC             error
						//	)
						//	// 请求下单
						//	binanceOrderRes3, orderInfoRes3, errC = requestBinanceOrder(tmpUpdateData.Symbol.(string), stopSide, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						//	if nil != errC || binanceOrderRes3.OrderId <= 0 {
						//		log.Println("龟兔，关仓，信息：", errC, binanceOrderRes3, orderInfoRes3, tmpUpdateData, stopSide, orderType, positionSide, quantity, tmpUser.Id)
						//	}
						//}

						//// 未变化，撤销订单
						//if binanceOrderRes2.OrderId > 0 && "NEW" == orderBinanceInfo.Status {
						//	var (
						//		binanceOrderResR *binanceOrder
						//		orderInfoResR    *orderInfo
						//	)
						//
						//	binanceOrderResR, orderInfoResR, err2 = requestBinanceDeleteOrder(tmpUpdateData.Symbol.(string), strconv.FormatUint(uint64(binanceOrderRes2.OrderId), 10), tmpUser.ApiKey, tmpUser.ApiSecret)
						//	if nil != err2 || binanceOrderResR.OrderId <= 0 {
						//		log.Println("撤销止盈单，止盈，信息：", err2, binanceOrderResR, orderInfoResR, binanceOrderRes2)
						//	}
						//}

						log.Println("新，更新仓位，完成：", err, quantity, tmpUser.Id)
						return
					})
					if nil != err {
						fmt.Println("龟兔，添加下单任务异常，新增仓位，错误信息：", err, tmpUpdateData, tmpUser)
					}
				} else if 2 == tmpUser.OrderType {
					if !symbolsMapGate.Contains(tmpUpdateData.Symbol.(string)) {
						fmt.Println("无效gate代币信息：", err, tmpUser, tmpUpdateData.Symbol.(string))
						continue
					}

					if 0 < symbolsMapGate.Get(tmpUpdateData.Symbol.(string)).(*SymbolGate).QuantoMultiplier {
						//log.Println("OrderAtPlat，交易对信息错误:", user, currentData, s.SymbolsMap.Get(symbolMapKey).(*entity.LhCoinSymbol))

						// 转化为张数=币的数量/每张币的数量
						tmpQtyGate := tmpQty / symbolsMapGate.Get(tmpUpdateData.Symbol.(string)).(*SymbolGate).QuantoMultiplier
						// 按张的精度转化，
						quantityInt64Gate := int64(math.Round(tmpQtyGate))
						if "LONG" == positionSide {

						} else {
							quantityInt64Gate = -quantityInt64Gate
						}

						err = s.pool.Add(ctx, func(ctx context.Context) {
							//defer wg.Done()
							current := time.Now()
							log.Println("当前时间：", current, "用户：", tmpUser.Id)

							endOfMinute := time.Date(
								current.Year(),
								current.Month(),
								current.Day(),
								current.Hour(),
								current.Minute(),
								59, // 秒设置为59
								0,  // 纳秒设为0
								current.Location(),
							)
							diffMillis := endOfMinute.Sub(current).Milliseconds()
							diffs := int32(diffMillis / 1000)
							if 0 >= diffs {
								diffs = 3
							}

							var (
								autoSize   string
								gateRes    gateapi.FuturesOrder
								symbolGate = symbolsMapGate.Get(tmpUpdateData.Symbol.(string)).(*SymbolGate).Symbol
							)

							gateRes, err = placeOrderGate(tmpUser.ApiKey, tmpUser.ApiSecret, symbolGate, quantityInt64Gate, false, autoSize)
							if nil != err || 0 >= gateRes.Id {
								log.Println("OrderAtPlat，Gate下单:", gateRes, quantityInt64Gate, symbolGate)
								return
							}

							//// 止盈价格
							//var (
							//	avgPrice   float64
							//	priceFloat float64
							//	price      string // 止盈价 委托价格
							//	errAA      error
							//)
							//avgPrice, errAA = strconv.ParseFloat(gateRes.FillPrice, 64)
							//if nil != errAA {
							//	log.Println("下单错误，avgPrive解析失败，gate", errAA)
							//	return
							//}
							//
							if "LONG" == positionSide {
								autoSize = "close_long"
								//	priceFloat = avgPrice + avgPrice*tmpUser.First
								//	//priceFloat = math.Round(priceFloat/exchangeInfoTickSize[tmpInsertData.Symbol.(string)]) * exchangeInfoTickSize[tmpInsertData.Symbol.(string)]
								//	//price = strconv.FormatFloat(priceFloat, 'f', symbolsMapGate.Get(tmpUpdateData.Symbol.(string)).(*SymbolGate).OrderPriceRound, 64)
								//	if 0 >= symbolsMapGate.Get(tmpUpdateData.Symbol.(string)).(*SymbolGate).OrderPriceRound {
								//		price = fmt.Sprintf("%d", int64(priceFloat))
								//	} else {
								//		price = strconv.FormatFloat(priceFloat, 'f', symbolsMapGate.Get(tmpUpdateData.Symbol.(string)).(*SymbolGate).OrderPriceRound, 64)
								//	}
								//
							} else {
								autoSize = "close_short"
								//	priceFloat = avgPrice - avgPrice*tmpUser.First
								//	if 0 >= symbolsMapGate.Get(tmpUpdateData.Symbol.(string)).(*SymbolGate).OrderPriceRound {
								//		price = fmt.Sprintf("%d", int64(priceFloat))
								//	} else {
								//		price = strconv.FormatFloat(priceFloat, 'f', symbolsMapGate.Get(tmpUpdateData.Symbol.(string)).(*SymbolGate).OrderPriceRound, 64)
								//	}
								//}
								//
								//var (
								//	gateRes2 gateapi.FuturesOrder
								//)
								//gateRes2, err = placeLimitCloseOrderGate(tmpUser.ApiKey, tmpUser.ApiSecret, symbolGate, price, autoSize)
								//if nil != err || 0 >= gateRes2.Id {
								//	log.Println("OrderAtPlat，Gate,限价下单:", gateRes2, quantityInt64Gate, symbolGate)
							}

							// 过了时间立马平掉
							if time.Now().After(endOfMinute) {
							} else {
								time.Sleep(time.Duration(diffMillis) * time.Millisecond)
							}

							var (
								gateRes3 gateapi.FuturesOrder
							)

							gateRes3, err = placeOrderGate(tmpUser.ApiKey, tmpUser.ApiSecret, symbolGate, 0, true, autoSize)
							if nil != err || 0 >= gateRes3.Id {
								log.Println("OrderAtPlat，Gate下单，平仓:", gateRes3, quantityInt64Gate, symbolGate)
							}

							//if gateRes2.Id > 0 {
							//	var (
							//		gateRes5 gateapi.FuturesOrder
							//	)
							//	gateRes5, err = getOrderGate(tmpUser.ApiKey, tmpUser.ApiSecret, strconv.FormatInt(gateRes2.Id, 10))
							//	if nil != err || 0 >= gateRes5.Id {
							//		log.Println("OrderAtPlat，Gate下单，查询限价:", gateRes5, quantityInt64Gate, symbolGate)
							//	}
							//
							//	fmt.Println(gateRes5)
							//
							//	if "open" == gateRes5.Status {
							//		var (
							//			gateRes4 gateapi.FuturesOrder
							//		)
							//		gateRes4, err = removeLimitCloseOrderGate(tmpUser.ApiKey, tmpUser.ApiSecret, strconv.FormatInt(gateRes2.Id, 10))
							//		if nil != err || 0 >= gateRes4.Id {
							//			log.Println("OrderAtPlat，Gate下单，撤销限价:", gateRes4, quantityInt64Gate, symbolGate)
							//		}
							//	}
							//}

							log.Println("新，更新仓位，完成：", err, quantity, tmpUser.Id)
							return
						})
						if nil != err {
							fmt.Println("龟兔，添加下单任务异常，新增仓位，错误信息：", err, quantityInt64Gate, tmpUser.Id)
						}

					}
				}
			}

			return true
		})

		// 回收协程
		//wg.Wait()

		log.Printf("龟兔，程序执行完毕，开始 %v, 拉取时长: %v, 总计时长: %v\n", start, timePull, time.Since(start))
	}
}

func floatGreater(a, b, epsilon float64) bool {
	return a-b >= epsilon
}

func (s *sBinanceTraderHistory) HandleKLine(ctx context.Context, slot uint64) {
	var (
		loc *time.Location
		err error
	)
	loc, err = time.LoadLocation("Asia/Shanghai")
	if err != nil {
		panic("无法加载 Asia/Shanghai 时区: " + err.Error())
	}

	if slot == 0 {
		slot = 1
	}

	now := time.Now().In(loc)

	// 对齐到当前时间的上一个 4 小时整点
	hour := now.Hour()
	alignedHour := (hour / 4) * 4
	currentSlotEnd := time.Date(now.Year(), now.Month(), now.Day(), alignedHour, 0, 0, 0, loc)

	// 计算目标 slot 的结束时间，减去1ms
	endTime := currentSlotEnd.Add(-time.Duration(slot-1) * 4 * time.Hour).Add(-time.Millisecond)
	startTime := endTime.Add(-4*time.Hour + time.Millisecond) // 加回那1ms，保持时长为4小时

	startMs := fmt.Sprintf("%d", startTime.UnixMilli())
	endMs := fmt.Sprintf("%d", endTime.UnixMilli())

	//fmt.Println("Start:", startTime)
	//fmt.Println("End:  ", endTime)
	//fmt.Println("StartMs:", startMs, "EndMs:", endMs)

	//var (
	//	currentCoinUsdt float64
	//)

	// 调用你的 K 线查询函数，symbol 为 ETHBTC，limit 为 1 天
	for _, v := range coins {
		// 不存在跳过
		tmpSymbol := v + "USDT"
		if !symbolsMap.Contains(tmpSymbol) {
			continue
		}

		tmpSq := symbolsMap.Get(tmpSymbol).(*entity.LhCoinSymbol).QuantityPrecision

		tmpCoinBtc := v + "BTC"

		var (
			kLines []*KLineDay
		)
		kLines, err = requestBinanceDailyKLines(tmpCoinBtc, startMs, endMs, "1")
		if err != nil {
			log.Println(err, "查询k线错误")
			continue
		}

		// 打印结果（使用中国时间显示）
		for _, k := range kLines {
			//fmt.Printf("币种：%s 日期: %s 开盘: %s 收盘: %s 最高: %s 最低: %s\n",
			//	v+"BTC",
			//	time.UnixMilli(k.OpenTime).In(loc).Format("2006-01-02"),
			//	k.Open, k.Close, k.High, k.Low)

			var (
				tmpCurrentPrice float64
			)
			tmpCurrentPrice, _ = strconv.ParseFloat(k.Close, 10)
			if 0 >= tmpCurrentPrice {
				fmt.Println("价格0", k)
				continue
			}

			// 初始化
			if !initPrice.Contains(tmpCoinBtc) {
				initPrice.Set(tmpCoinBtc, tmpCurrentPrice)
				log.Println("币种：", tmpCoinBtc, k.Close, tmpCurrentPrice)
				continue
			}

			tmpInitPrice := initPrice.Get(tmpCoinBtc).(float64)

			// 大于等于1e-8
			if floatGreater(tmpCurrentPrice, tmpInitPrice, 1e-8) {
				// 涨价

				// 涨价百分之几
				tmpSubRate := (tmpCurrentPrice - tmpInitPrice) / tmpInitPrice
				if !floatGreater(tmpSubRate, 0.01, 1e-4) {
					// 不到百二忽略
					continue
				}

				// 大于等于百二，例如：相差0.0001识别
				// 更新初始化价格
				initPrice.Set(tmpCoinBtc, tmpCurrentPrice)

				// 下单 开空
				var (
					price         float64
					coinUsdtPrice *FuturesPrice
				)
				coinUsdtPrice, err = getUSDMFuturesPrice(tmpSymbol)
				if nil != err {
					log.Println("价格查询错误", err)
					continue
				}

				price, err = strconv.ParseFloat(coinUsdtPrice.Price, 10)
				if 0 >= price {
					fmt.Println("价格0，usdt", k)
					continue
				}

				// 开仓数量
				tmpQty := 6 * tmpSubRate * 100 / price

				// 精度调整
				var (
					quantity      string
					quantityFloat float64
				)
				if 0 >= tmpSq {
					quantity = fmt.Sprintf("%d", int64(tmpQty))
				} else {
					quantity = strconv.FormatFloat(tmpQty, 'f', tmpSq, 64)
				}

				quantityFloat, err = strconv.ParseFloat(quantity, 64)
				if nil != err {
					log.Println(err)
					continue
				}

				// binance
				var (
					binanceOrderRes *binanceOrder
					orderInfoRes    *orderInfo
					errA            error
				)
				for tmpI := int64(1); tmpI <= 1; tmpI++ {
					tmpIStr := strconv.FormatInt(tmpI, 10)
					tmpKey := tmpCoinBtc + tmpIStr

					tmpApiK := HandleKLineApiKey
					tmpApiS := HandleKLineApiSecret
					//if 2 == tmpI {
					//	tmpApiK = HandleKLineApiKeyTwo
					//	tmpApiS = HandleKLineApiSecretTwo
					//}

					if !lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
						// 请求下单
						binanceOrderRes, orderInfoRes, errA = requestBinanceOrder(tmpSymbol, "SELL", "MARKET", "SHORT", quantity, tmpApiK, tmpApiS)
						if nil != errA || binanceOrderRes.OrderId <= 0 {
							log.Println("仓位，信息：", errA, binanceOrderRes, orderInfoRes, quantity)
						} else {
							if orderMap.Contains(tmpKey) {
								d1 := decimal.NewFromFloat(orderMap.Get(tmpKey).(float64))
								d2 := decimal.NewFromFloat(quantityFloat)
								result := d1.Add(d2)

								var (
									newRes float64
									exact  bool
								)
								newRes, exact = result.Float64()
								if !exact {
									fmt.Println("转换过程中可能发生了精度损失", d1, d2, quantityFloat, orderMap.Get(tmpKey).(float64), newRes)
								}

								orderMap.Set(tmpKey, newRes)
							} else {
								orderMap.Set(tmpKey, quantityFloat)
							}

							//currentCoinUsdt += quantityFloat * price
						}

						time.Sleep(1 * time.Second)
					}

					fmtOrderMap := float64(0)
					if orderMap.Contains(tmpKey) {
						fmtOrderMap = orderMap.Get(tmpKey).(float64)
					}

					log.Println("价格涨了百1以上", v, k.Close, tmpCurrentPrice, tmpInitPrice, "下单数量：", 6*tmpSubRate*100, price, tmpQty, tmpSq, quantityFloat, "仓位：", fmtOrderMap, binanceOrderRes, orderInfoRes, errA)
				}

			} else if floatGreater(tmpInitPrice, tmpCurrentPrice, 1e-8) {
				// 掉价
				// 掉价百分之几
				tmpSubRate := (tmpInitPrice - tmpCurrentPrice) / tmpInitPrice
				if !floatGreater(tmpSubRate, 0.01, 1e-4) {
					// 不到百二忽略
					continue
				}

				// 小于等于百二，例如：相差0.0001识别
				// 更新初始化价格
				initPrice.Set(tmpCoinBtc, tmpCurrentPrice)

				// 下单 平空
				var (
					price         float64
					coinUsdtPrice *FuturesPrice
				)
				coinUsdtPrice, err = getUSDMFuturesPrice(tmpSymbol)
				if nil != err {
					log.Println("价格查询错误", err)
					continue
				}

				price, err = strconv.ParseFloat(coinUsdtPrice.Price, 10)
				if 0 >= price {
					fmt.Println("价格0，usdt", k)
					continue
				}

				for tmpI := int64(1); tmpI <= 1; tmpI++ {
					tmpIStr := strconv.FormatInt(tmpI, 10)
					tmpKey := tmpCoinBtc + tmpIStr

					tmpApiK := HandleKLineApiKey
					tmpApiS := HandleKLineApiSecret
					//if 2 == tmpI {
					//	tmpApiK = HandleKLineApiKeyTwo
					//	tmpApiS = HandleKLineApiSecretTwo
					//}

					// 平仓
					if !orderMap.Contains(tmpKey) {
						// 无仓位
						continue
					}

					// 剩余仓位数量
					tmpOrderQty := orderMap.Get(tmpKey).(float64)
					if lessThanOrEqualZero(tmpOrderQty, 0, 1e-7) {
						// 数量0
						continue
					}

					// 平仓数量
					tmpQty := 6 * tmpSubRate * 100 / price
					if floatGreater(tmpQty, tmpOrderQty, 1e-8) {
						tmpQty = tmpOrderQty
					}

					// 精度调整
					var (
						quantity      string
						quantityFloat float64
					)
					if 0 >= tmpSq {
						quantity = fmt.Sprintf("%d", int64(tmpQty))
					} else {
						quantity = strconv.FormatFloat(tmpQty, 'f', tmpSq, 64)
					}

					quantityFloat, err = strconv.ParseFloat(quantity, 64)
					if nil != err {
						log.Println(err)
						continue
					}

					// binance
					var (
						binanceOrderRes *binanceOrder
						orderInfoRes    *orderInfo
						errA            error
					)

					if !lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
						// 请求下单
						binanceOrderRes, orderInfoRes, errA = requestBinanceOrder(tmpSymbol, "BUY", "MARKET", "SHORT", quantity, tmpApiK, tmpApiS)
						if nil != errA || binanceOrderRes.OrderId <= 0 {
							log.Println("仓位，信息：", errA, binanceOrderRes, orderInfoRes, quantity)
						} else {
							if orderMap.Contains(tmpKey) {
								d1 := decimal.NewFromFloat(orderMap.Get(tmpKey).(float64))
								d2 := decimal.NewFromFloat(quantityFloat)
								result := d1.Sub(d2)

								var (
									newRes float64
									exact  bool
								)
								newRes, exact = result.Float64()
								if !exact {
									fmt.Println("转换过程中可能发生了精度损失", d1, d2, quantityFloat, orderMap.Get(tmpKey).(float64), newRes)
								}

								if lessThanOrEqualZero(newRes, 0, 1e-7) {
									newRes = 0
								}

								orderMap.Set(tmpKey, newRes)
							} else {
								orderMap.Set(tmpKey, quantityFloat)
							}

							//currentCoinUsdt -= quantityFloat * price
						}

						time.Sleep(1 * time.Second)
					}

					fmtOrderMap := float64(0)
					if orderMap.Contains(tmpKey) {
						fmtOrderMap = orderMap.Get(tmpKey).(float64)
					}

					log.Println("价格跌了百1以上", v, k.Close, tmpCurrentPrice, tmpInitPrice, "下单数量：", 6*tmpSubRate*100, price, tmpQty, tmpSq, quantityFloat, "仓位：", fmtOrderMap, binanceOrderRes, orderInfoRes, errA)
				}
			} else {
				fmt.Println("价格没变", v, tmpCurrentPrice, tmpInitPrice)
				continue
			}
		}
	}

	//log.Println("本次：coin usdt ", currentCoinUsdt)
	//orderMap.Iterator(func(k interface{}, v interface{}) bool {
	//	fmt.Println("用户仓位，测试结果:", k, v)
	//	return true
	//})

	// 查询全部持仓

	var (
		priceAll map[string]float64
	)
	priceAll, err = getAllUSDMFuturesPrices()
	if nil != err {
		fmt.Println("价格查询错误")
		return
	}

	for tmpI := int64(1); tmpI <= 1; tmpI++ {
		tmpApiK := HandleKLineApiKey
		tmpApiS := HandleKLineApiSecret
		//if 2 == tmpI {
		//	tmpApiK = HandleKLineApiKeyTwo
		//	tmpApiS = HandleKLineApiSecretTwo
		//}

		var (
			positions     []*BinancePosition
			coinTotalUsdt float64
			btcUsdt       float64
			btcPrice      float64
		)

		positions = getBinancePositionInfo(tmpApiK, tmpApiS)
		for _, v := range positions {
			// 新增
			var (
				currentAmount float64
			)
			currentAmount, err = strconv.ParseFloat(v.PositionAmt, 64)
			if nil != err {
				log.Println("c获取用户仓位接口，解析出错", v)
				return
			}

			if "BTCUSDT" == v.Symbol {
				if _, ok := priceAll[v.Symbol]; !ok {
					log.Println("价格不存在，btc开关仓", v)
					return
				}

				if "LONG" != v.PositionSide {
					continue
				}

				btcPrice = priceAll[v.Symbol]
				currentAmount = math.Abs(currentAmount)
				btcUsdt = priceAll[v.Symbol] * currentAmount

				log.Println(currentAmount, btcPrice)
				continue
			}

			if floatEqual(currentAmount, 0, 1e-7) {
				continue
			}

			if _, ok := priceAll[v.Symbol]; !ok {
				log.Println("价格不存在，btc开关仓", v)
				return
			}

			currentAmount = math.Abs(currentAmount)
			coinTotalUsdt += priceAll[v.Symbol] * currentAmount

			//var (
			//	symbolRel     = v.Symbol
			//	tmpQty        float64
			//	quantity      string
			//	quantityFloat float64
			//	orderType     = "MARKET"
			//	side          string
			//)
			//if "LONG" == v.PositionSide {
			//	side = "SELL"
			//} else if "SHORT" == v.PositionSide {
			//	side = "BUY"
			//} else {
			//	log.Println("close positions 仓位错误", v, vUser)
			//	continue
			//}
			//
			//tmpQty = currentAmount // 本次开单数量
			//if !symbolsMap.Contains(symbolRel) {
			//	log.Println("close positions，代币信息无效，信息", v, vUser)
			//	continue
			//}
			//
			//// 精度调整
			//if 0 >= symbolsMap.Get(symbolRel).(*LhCoinSymbol).QuantityPrecision {
			//	quantity = fmt.Sprintf("%d", int64(tmpQty))
			//} else {
			//	quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(symbolRel).(*LhCoinSymbol).QuantityPrecision, 64)
			//}
			//
			//quantityFloat, err = strconv.ParseFloat(quantity, 64)
			//if nil != err {
			//	log.Println("close positions，数量解析", v, vUser, err)
			//	continue
			//}
			//
			//if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
			//	continue
			//}
			//
			//var (
			//	binanceOrderRes *binanceOrder
			//	orderInfoRes    *orderInfo
			//)
			//
			//// 请求下单
			//binanceOrderRes, orderInfoRes, err = requestBinanceOrder(symbolRel, side, orderType, v.PositionSide, quantity, vUser.ApiKey, vUser.ApiSecret)
			//if nil != err {
			//	log.Println("close positions，执行下单错误，手动：", err, symbolRel, side, orderType, v.PositionSide, quantity, vUser.ApiKey, vUser.ApiSecret)
			//}
			//
			//// 下单异常
			//if 0 >= binanceOrderRes.OrderId {
			//	log.Println("自定义下单，binance下单错误：", orderInfoRes)
			//	continue
			//}
			//log.Println("close, 执行成功：", vUser, v, binanceOrderRes)
		}

		if 100 < coinTotalUsdt-btcUsdt {
			// 开
			tmp := coinTotalUsdt - btcUsdt
			tmpQty := tmp / btcPrice

			// 精度调整
			var (
				quantity      string
				quantityFloat float64
				tmpSq         = symbolsMap.Get("BTCUSDT").(*entity.LhCoinSymbol).QuantityPrecision
			)
			if 0 >= tmpSq {
				quantity = fmt.Sprintf("%d", int64(tmpQty))
			} else {
				quantity = strconv.FormatFloat(tmpQty, 'f', tmpSq, 64)
			}

			quantityFloat, err = strconv.ParseFloat(quantity, 64)
			if nil != err {
				log.Println("btc", err)
				continue
			}

			if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
				log.Println("btc open 开仓太小", tmpQty, quantityFloat)
				continue
			}

			// binance
			var (
				binanceOrderRes *binanceOrder
				orderInfoRes    *orderInfo
				errA            error
			)

			if !lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
				// 请求下单
				binanceOrderRes, orderInfoRes, errA = requestBinanceOrder("BTCUSDT", "BUY", "MARKET", "LONG", quantity, tmpApiK, tmpApiS)
				//if nil != errA || binanceOrderRes.OrderId <= 0 {
				//	log.Println("仓位，信息：", errA, binanceOrderRes, orderInfoRes, quantity)
				//} else {
				//}

				time.Sleep(1 * time.Second)
			}

			log.Println("本次开：btc usdt ", coinTotalUsdt, btcUsdt, tmpQty, quantity, btcPrice, "数量：", quantityFloat, binanceOrderRes, orderInfoRes, errA, tmpSq)

		} else if 1 < btcUsdt-coinTotalUsdt {
			// 关
			tmp := btcUsdt - coinTotalUsdt
			tmpQty := tmp / btcPrice

			// 精度调整
			var (
				quantity      string
				quantityFloat float64
				tmpSq         = symbolsMap.Get("BTCUSDT").(*entity.LhCoinSymbol).QuantityPrecision
			)
			if 0 >= tmpSq {
				quantity = fmt.Sprintf("%d", int64(tmpQty))
			} else {
				quantity = strconv.FormatFloat(tmpQty, 'f', tmpSq, 64)
			}

			quantityFloat, err = strconv.ParseFloat(quantity, 64)
			if nil != err {
				log.Println("btc close", err)
				continue
			}

			if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
				log.Println("btc close 关仓太小", tmpQty, quantityFloat)
				continue
			}

			// binance
			var (
				binanceOrderRes *binanceOrder
				orderInfoRes    *orderInfo
				errA            error
			)

			if !lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
				// 请求下单
				binanceOrderRes, orderInfoRes, errA = requestBinanceOrder("BTCUSDT", "SELL", "MARKET", "LONG", quantity, tmpApiK, tmpApiS)
				//if nil != errA || binanceOrderRes.OrderId <= 0 {
				//	log.Println("仓位，信息：", errA, binanceOrderRes, orderInfoRes, quantity)
				//} else {
				//
				//}

				time.Sleep(1 * time.Second)
			}

			log.Println("本次关：btc usdt ", coinTotalUsdt, btcUsdt, tmpQty, quantity, btcPrice, "数量：", quantityFloat, binanceOrderRes, orderInfoRes, errA, tmpSq)

		}

		//var (
		//	symbolRel     = v.Symbol
		//	tmpQty        float64
		//	quantity      string
		//	quantityFloat float64
		//	orderType     = "MARKET"
		//	side          string
		//)
		//if "LONG" == v.PositionSide {
		//	side = "SELL"
		//} else if "SHORT" == v.PositionSide {
		//	side = "BUY"
		//} else {
		//	log.Println("close positions 仓位错误", v, vUser)
		//	continue
		//}
		//
		//tmpQty = currentAmount // 本次开单数量
		//if !symbolsMap.Contains(symbolRel) {
		//	log.Println("close positions，代币信息无效，信息", v, vUser)
		//	continue
		//}
		//
		//// 精度调整
		//if 0 >= symbolsMap.Get(symbolRel).(*LhCoinSymbol).QuantityPrecision {
		//	quantity = fmt.Sprintf("%d", int64(tmpQty))
		//} else {
		//	quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(symbolRel).(*LhCoinSymbol).QuantityPrecision, 64)
		//}
		//
		//quantityFloat, err = strconv.ParseFloat(quantity, 64)
		//if nil != err {
		//	log.Println("close positions，数量解析", v, vUser, err)
		//	continue
		//}
		//
		//if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
		//	continue
		//}
		//
		//var (
		//	binanceOrderRes *binanceOrder
		//	orderInfoRes    *orderInfo
		//)
		//
		//// 请求下单
		//binanceOrderRes, orderInfoRes, err = requestBinanceOrder(symbolRel, side, orderType, v.PositionSide, quantity, vUser.ApiKey, vUser.ApiSecret)
		//if nil != err {
		//	log.Println("close positions，执行下单错误，手动：", err, symbolRel, side, orderType, v.PositionSide, quantity, vUser.ApiKey, vUser.ApiSecret)
		//}
		//
		//// 下单异常
		//if 0 >= binanceOrderRes.OrderId {
		//	log.Println("自定义下单，binance下单错误：", orderInfoRes)
		//	continue
		//}
		//log.Println("close, 执行成功：", vUser, v, binanceOrderRes)
	}

	// 本次
	//if currentCoinUsdt-0 > 1e-7 {
	//	// 超过额度了，开仓
	//	coinUsdtOrder.Add(currentCoinUsdt)
	//	if floatGreater(coinUsdtOrder.Val(), btcUsdtOrder.Val(), 13-7) {
	//		currentCoinUsdt = coinUsdtOrder.Val() - btcUsdtOrder.Val()
	//
	//		// 开150u的btc
	//		tmp150Num := uint64(math.Abs(currentCoinUsdt))/150 + 1
	//		tmpOpenBtcUsdt := float64(tmp150Num * 150)
	//
	//		// 下单 平空
	//		var (
	//			price         float64
	//			coinUsdtPrice *FuturesPrice
	//		)
	//		coinUsdtPrice, err = getUSDMFuturesPrice("BTCUSDT")
	//		if nil != err {
	//			log.Println("价格查询错误,btc开", err)
	//			return
	//		}
	//
	//		price, err = strconv.ParseFloat(coinUsdtPrice.Price, 10)
	//		if 0 >= price {
	//			fmt.Println("价格0，usdt，btcusdt")
	//			return
	//		}
	//
	//		tmpQty := tmpOpenBtcUsdt / price
	//
	//		// 精度调整
	//		var (
	//			quantity      string
	//			quantityFloat float64
	//			tmpSq         = symbolsMap.Get("BTCUSDT").(*entity.LhCoinSymbol).QuantityPrecision
	//		)
	//		if 0 >= tmpSq {
	//			quantity = fmt.Sprintf("%d", int64(tmpQty))
	//		} else {
	//			quantity = strconv.FormatFloat(tmpQty, 'f', tmpSq, 64)
	//		}
	//
	//		quantityFloat, err = strconv.ParseFloat(quantity, 64)
	//		if nil != err {
	//			log.Println("btc", err)
	//			return
	//		}
	//
	//		// binance
	//		var (
	//			binanceOrderRes *binanceOrder
	//			orderInfoRes    *orderInfo
	//			errA            error
	//		)
	//
	//		if !lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
	//			// 请求下单
	//			binanceOrderRes, orderInfoRes, errA = requestBinanceOrder("BTCUSDT", "BUY", "MARKET", "LONG", quantity, HandleKLineApiKey, HandleKLineApiSecret)
	//			if nil != errA || binanceOrderRes.OrderId <= 0 {
	//				log.Println("仓位，信息：", errA, binanceOrderRes, orderInfoRes, quantity)
	//			} else {
	//				// 额度增长
	//				btcUsdtOrder.Add(tmpOpenBtcUsdt)
	//
	//				if orderMap.Contains("BTCUSDT") {
	//					d1 := decimal.NewFromFloat(orderMap.Get("BTCUSDT").(float64))
	//					d2 := decimal.NewFromFloat(quantityFloat)
	//					result := d1.Add(d2)
	//
	//					var (
	//						newRes float64
	//						exact  bool
	//					)
	//					newRes, exact = result.Float64()
	//					if !exact {
	//						fmt.Println("转换过程中可能发生了精度损失", d1, d2, quantityFloat, orderMap.Get("BTCUSDT").(float64), newRes)
	//					}
	//
	//					orderMap.Set("BTCUSDT", newRes)
	//				} else {
	//					orderMap.Set("BTCUSDT", quantityFloat)
	//				}
	//			}
	//
	//			time.Sleep(1 * time.Second)
	//		}
	//
	//		fmtOrderMap := float64(0)
	//		if orderMap.Contains("BTCUSDT") {
	//			fmtOrderMap = orderMap.Get("BTCUSDT").(float64)
	//		}
	//
	//		log.Println("本次开：btc usdt ", tmpOpenBtcUsdt, price, "数量：", quantityFloat, binanceOrderRes, orderInfoRes, errA, tmpQty, quantity, tmpSq, "仓位：", fmtOrderMap)
	//	} else {
	//		// 不需要开仓
	//		log.Println("不需要开", coinUsdtOrder.Val(), currentCoinUsdt, btcUsdtOrder.Val())
	//		return
	//	}
	//
	//} else if 0-currentCoinUsdt > 1e-7 {
	//	coinUsdtOrder.Add(currentCoinUsdt)
	//
	//	// 关仓
	//	if btcUsdtOrder.Val()-150 < 1e-7 {
	//		// 不足150u不用关
	//		log.Println("不需要关，不足150u的btc", btcUsdtOrder.Val())
	//		return
	//	}
	//
	//	tmpOpenBtcUsdt := math.Abs(currentCoinUsdt)
	//	// 保留150最低
	//	if (btcUsdtOrder.Val()-150)+currentCoinUsdt < 1e-7 {
	//		tmpOpenBtcUsdt = math.Abs(btcUsdtOrder.Val() - 150)
	//	}
	//
	//	if lessThanOrEqualZero(tmpOpenBtcUsdt, 0, 1e-7) {
	//		log.Println("不需要关，不足150u的btc", tmpOpenBtcUsdt)
	//		return
	//	}
	//
	//	// 平仓
	//	if !orderMap.Contains("BTCUSDT") {
	//		// 无仓位
	//		log.Println("无仓位不科学", err)
	//		return
	//	}
	//
	//	// 剩余仓位数量
	//	tmpOrderQty := orderMap.Get("BTCUSDT").(float64)
	//	if lessThanOrEqualZero(tmpOrderQty, 0, 1e-7) {
	//		// 数量0
	//		log.Println("数量0不科学", err)
	//		return
	//	}
	//
	//	// 下单 平空
	//	var (
	//		price         float64
	//		coinUsdtPrice *FuturesPrice
	//	)
	//	coinUsdtPrice, err = getUSDMFuturesPrice("BTCUSDT")
	//	if nil != err {
	//		log.Println("价格查询错误,btc关", err)
	//		return
	//	}
	//
	//	price, err = strconv.ParseFloat(coinUsdtPrice.Price, 10)
	//	if 0 >= price {
	//		fmt.Println("价格0，usdt，btcusdt")
	//		return
	//	}
	//
	//	tmpQty := tmpOpenBtcUsdt / price
	//	if floatGreater(tmpQty, tmpOrderQty, 1e-8) {
	//		tmpQty = tmpOrderQty
	//	}
	//
	//	// 精度调整
	//	var (
	//		quantity      string
	//		quantityFloat float64
	//		tmpSq         = symbolsMap.Get("BTCUSDT").(*entity.LhCoinSymbol).QuantityPrecision
	//	)
	//	if 0 >= tmpSq {
	//		quantity = fmt.Sprintf("%d", int64(tmpQty))
	//	} else {
	//		quantity = strconv.FormatFloat(tmpQty, 'f', tmpSq, 64)
	//	}
	//
	//	quantityFloat, err = strconv.ParseFloat(quantity, 64)
	//	if nil != err {
	//		log.Println("btc close", err)
	//		return
	//	}
	//
	//	// binance
	//	var (
	//		binanceOrderRes *binanceOrder
	//		orderInfoRes    *orderInfo
	//		errA            error
	//	)
	//
	//	if !lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
	//		// 请求下单
	//		binanceOrderRes, orderInfoRes, errA = requestBinanceOrder("BTCUSDT", "SELL", "MARKET", "LONG", quantity, HandleKLineApiKey, HandleKLineApiSecret)
	//		if nil != errA || binanceOrderRes.OrderId <= 0 {
	//			log.Println("仓位，信息：", errA, binanceOrderRes, orderInfoRes, quantity)
	//		} else {
	//			// 额度减少
	//			btcUsdtOrder.Add(-tmpOpenBtcUsdt)
	//
	//			if orderMap.Contains("BTCUSDT") {
	//				d1 := decimal.NewFromFloat(orderMap.Get("BTCUSDT").(float64))
	//				d2 := decimal.NewFromFloat(quantityFloat)
	//				result := d1.Sub(d2)
	//
	//				var (
	//					newRes float64
	//					exact  bool
	//				)
	//				newRes, exact = result.Float64()
	//				if !exact {
	//					fmt.Println("转换过程中可能发生了精度损失", d1, d2, quantityFloat, orderMap.Get("BTCUSDT").(float64), newRes)
	//				}
	//
	//				if lessThanOrEqualZero(newRes, 0, 1e-7) {
	//					newRes = 0
	//				}
	//
	//				orderMap.Set("BTCUSDT", newRes)
	//			} else {
	//				orderMap.Set("BTCUSDT", quantityFloat)
	//			}
	//		}
	//
	//		time.Sleep(1 * time.Second)
	//	}
	//
	//	fmtOrderMap := float64(0)
	//	if orderMap.Contains("BTCUSDT") {
	//		fmtOrderMap = orderMap.Get("BTCUSDT").(float64)
	//	}
	//
	//	log.Println("本次关：btc usdt ", tmpOpenBtcUsdt, price, "数量：", quantityFloat, binanceOrderRes, orderInfoRes, errA, tmpQty, quantity, tmpSq, "仓位：", fmtOrderMap)
	//} else {
	//	return
	//}
}

// BinancePosition 代表单个头寸（持仓）信息
type BinancePosition struct {
	Symbol                 string `json:"symbol"`                 // 交易对
	InitialMargin          string `json:"initialMargin"`          // 当前所需起始保证金(基于最新标记价格)
	MaintMargin            string `json:"maintMargin"`            // 维持保证金
	UnrealizedProfit       string `json:"unrealizedProfit"`       // 持仓未实现盈亏
	PositionInitialMargin  string `json:"positionInitialMargin"`  // 持仓所需起始保证金(基于最新标记价格)
	OpenOrderInitialMargin string `json:"openOrderInitialMargin"` // 当前挂单所需起始保证金(基于最新标记价格)
	Leverage               string `json:"leverage"`               // 杠杆倍率
	Isolated               bool   `json:"isolated"`               // 是否是逐仓模式
	EntryPrice             string `json:"entryPrice"`             // 持仓成本价
	MaxNotional            string `json:"maxNotional"`            // 当前杠杆下用户可用的最大名义价值
	BidNotional            string `json:"bidNotional"`            // 买单净值，忽略
	AskNotional            string `json:"askNotional"`            // 卖单净值，忽略
	PositionSide           string `json:"positionSide"`           // 持仓方向 (BOTH, LONG, SHORT)
	PositionAmt            string `json:"positionAmt"`            // 持仓数量
	UpdateTime             int64  `json:"updateTime"`             // 更新时间
}

// floatEqual 判断两个浮点数是否在精度范围内相等
func floatEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) <= epsilon
}

// 获取币安服务器时间
func getBinanceServerTime() int64 {
	urlTmp := "https://api.binance.com/api/v3/time"
	resp, err := http.Get(urlTmp)
	if err != nil {
		log.Println("Error getting server time:", err)
		return 0
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				log.Println("关闭响应体错误：", err)
			}
		}
	}()

	var serverTimeResponse struct {
		ServerTime int64 `json:"serverTime"`
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response body:", err)
		return 0
	}
	if err := json.Unmarshal(body, &serverTimeResponse); err != nil {
		log.Println("Error unmarshaling server time:", err)
		return 0
	}

	return serverTimeResponse.ServerTime
}

// 生成签名
func generateSignature(apiS string, params url.Values) string {
	// 将请求参数编码成 URL 格式的字符串
	queryString := params.Encode()

	// 生成签名
	mac := hmac.New(sha256.New, []byte(apiS))
	mac.Write([]byte(queryString)) // 用 API Secret 生成签名
	return hex.EncodeToString(mac.Sum(nil))
}

// BinanceResponse 包含多个仓位和账户信息
type BinanceResponse struct {
	Positions []*BinancePosition `json:"positions"` // 仓位信息
}

// getBinancePositionInfo 获取账户信息
func getBinancePositionInfo(apiK, apiS string) []*BinancePosition {
	// 请求的API地址
	endpoint := "/fapi/v2/account"
	baseURL := "https://fapi.binance.com"

	// 获取当前时间戳（使用服务器时间避免时差问题）
	serverTime := getBinanceServerTime()
	if serverTime == 0 {
		return nil
	}
	timestamp := strconv.FormatInt(serverTime, 10)

	// 设置请求参数
	params := url.Values{}
	params.Set("timestamp", timestamp)
	params.Set("recvWindow", "5000") // 设置接收窗口

	// 生成签名
	signature := generateSignature(apiS, params)

	// 将签名添加到请求参数中
	params.Set("signature", signature)

	// 构建完整的请求URL
	requestURL := baseURL + endpoint + "?" + params.Encode()

	// 创建请求
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		log.Println("Error creating request:", err)
		return nil
	}

	// 添加请求头
	req.Header.Add("X-MBX-APIKEY", apiK)

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error sending request:", err)
		return nil
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				log.Println("关闭响应体错误：", err)
			}
		}
	}()

	// 读取响应
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response:", err)
		return nil
	}

	// 解析响应
	var o *BinanceResponse
	err = json.Unmarshal(body, &o)
	if err != nil {
		log.Println("Error unmarshalling response:", err)
		return nil
	}

	// 返回资产余额
	return o.Positions
}

// CloseBinanceUserPositions close binance user positions
func (s *sBinanceTraderHistory) CloseBinanceUserPositions(ctx context.Context) uint64 {
	var (
		err error
	)

	for tmpI := int64(1); tmpI <= 1; tmpI++ {

		tmpApiK := HandleKLineApiKey
		tmpApiS := HandleKLineApiSecret
		//if 2 == tmpI {
		//	tmpApiK = HandleKLineApiKeyTwo
		//	tmpApiS = HandleKLineApiSecretTwo
		//}

		var (
			positions []*BinancePosition
		)

		positions = getBinancePositionInfo(tmpApiK, tmpApiS)
		for _, v := range positions {
			// 新增
			var (
				currentAmount float64
			)
			currentAmount, err = strconv.ParseFloat(v.PositionAmt, 64)
			if nil != err {
				log.Println("close positions 获取用户仓位接口，解析出错", v)
				continue
			}

			currentAmount = math.Abs(currentAmount)
			if floatEqual(currentAmount, 0, 1e-7) {
				continue
			}

			var (
				symbolRel     = v.Symbol
				tmpQty        float64
				quantity      string
				quantityFloat float64
				orderType     = "MARKET"
				side          string
			)
			if "LONG" == v.PositionSide {
				side = "SELL"
			} else if "SHORT" == v.PositionSide {
				side = "BUY"
			} else {
				log.Println("close positions 仓位错误", v)
				continue
			}

			tmpQty = currentAmount // 本次开单数量
			if !symbolsMap.Contains(symbolRel) {
				log.Println("close positions，代币信息无效，信息", v)
				continue
			}

			// 精度调整
			if 0 >= symbolsMap.Get(symbolRel).(*entity.LhCoinSymbol).QuantityPrecision {
				quantity = fmt.Sprintf("%d", int64(tmpQty))
			} else {
				quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(symbolRel).(*entity.LhCoinSymbol).QuantityPrecision, 64)
			}

			quantityFloat, err = strconv.ParseFloat(quantity, 64)
			if nil != err {
				log.Println("close positions，数量解析", v, err)
				continue
			}

			if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
				continue
			}

			var (
				binanceOrderRes *binanceOrder
				orderInfoRes    *orderInfo
			)

			// 请求下单
			binanceOrderRes, orderInfoRes, err = requestBinanceOrder(symbolRel, side, orderType, v.PositionSide, quantity, tmpApiK, tmpApiS)
			if nil != err {
				log.Println("close positions，执行下单错误，手动：", err, symbolRel, side, orderType, v.PositionSide, quantity, tmpApiK, tmpApiS)
			}

			// 下单异常
			if 0 >= binanceOrderRes.OrderId {
				log.Println("自定义下单，binance下单错误：", orderInfoRes)
				continue
			}
			log.Println("close, 执行成功：", v, binanceOrderRes)

			time.Sleep(500 * time.Millisecond)
		}
	}

	return 1
}

// HandleOrderAndOrder2 处理止盈和止损单
func (s *sBinanceTraderHistory) HandleOrderAndOrder2(ctx context.Context) bool {

	// 止盈
	unlockLastOrder := make([]string, 0)
	lastOrders.Iterator(func(k interface{}, v interface{}) bool {
		// 无单
		if nil == v {
			return true
		}

		tmpOrders := v.(*lastOrderStruct)
		// 有止盈，止损，存在
		if lastOrders2.Contains(k) && nil != lastOrders2.Get(k) {
			// 查询进度
			var (
				orderBinanceInfo *binanceOrder
				err              error
			)
			orderBinanceInfo, err = requestBinanceOrderInfo(tmpOrders.orderInfo.Symbol, tmpOrders.orderId, tmpOrders.user.ApiKey, tmpOrders.user.ApiSecret)
			if nil != err || nil == orderBinanceInfo {
				fmt.Println("查询止盈单信息失败：", orderBinanceInfo, err, tmpOrders.orderInfo.Symbol, tmpOrders.orderId)
				return true
			}

			// 订单变更
			if 0 < len(orderBinanceInfo.Status) && "NEW" != orderBinanceInfo.Status {
				unlockLastOrder = append(unlockLastOrder, k.(string))
			}

		} else {
			// 有止盈，无止损，不存在先观察一下时间，避免还未下单的误判
			currentTmp := time.Now()
			// 是否开仓10秒以上了
			if math.Abs(currentTmp.Sub(tmpOrders.time).Seconds()) <= 10 {
				return true
			}

			// 10s以上还不存在，应该是止损单异常了，平掉当前止盈单
			var (
				//		binanceOrderRes2 *binanceOrder
				binanceOrderRes3 *binanceOrder
				//		orderInfoRes2    *orderInfo
				orderInfoRes3 *orderInfo
				err           error
			)

			binanceOrderRes3, orderInfoRes3, err = requestBinanceDeleteOrder(tmpOrders.orderInfo.Symbol, tmpOrders.orderId, tmpOrders.user.ApiKey, tmpOrders.user.ApiSecret)
			if nil != err || binanceOrderRes3.OrderId <= 0 {
				fmt.Println("任务，撤销下单，止盈，信息：", err, binanceOrderRes3, orderInfoRes3, tmpOrders.orderInfo)
			}

			// 无论如何清除lastOrder
			unlockLastOrder = append(unlockLastOrder, k.(string))
			return true
		}

		return true
	})
	for _, vUnlock := range unlockLastOrder {
		fmt.Println("删除lastOrder：", vUnlock)
		lastOrders.Set(vUnlock, nil)
	}

	// 止损只需要判断异常的就可以了
	unlockLastOrder2 := make([]string, 0)
	lastOrders2.Iterator(func(k interface{}, v interface{}) bool {
		// 无单
		if nil == v {
			return true
		}

		tmpOrders2 := v.(*lastOrderStruct)
		// 正常情况会在lastOrder的遍历中执行，有止盈，止损，存在
		if lastOrders.Contains(k) && nil != lastOrders.Get(k) {
			// 查询进度
			var (
				orderBinanceInfo *binanceOrder
				err              error
			)
			orderBinanceInfo, err = requestBinanceOrderInfo(tmpOrders2.orderInfo.Symbol, tmpOrders2.orderId, tmpOrders2.user.ApiKey, tmpOrders2.user.ApiSecret)
			if nil != err || nil == orderBinanceInfo {
				fmt.Println("查询止损单信息失败：", orderBinanceInfo, err, tmpOrders2.orderInfo.Symbol, tmpOrders2.orderId)
				return true
			}

			// 订单变更
			if 0 < len(orderBinanceInfo.Status) && "NEW" != orderBinanceInfo.Status {
				unlockLastOrder2 = append(unlockLastOrder2, k.(string))
			}
		} else {
			// 有止损，无止盈，不存在先观察一下时间，避免还未下单的误判
			currentTmp := time.Now()
			// 是否开仓10秒以上了
			if math.Abs(currentTmp.Sub(tmpOrders2.time).Seconds()) <= 10 {
				return true
			}

			// 10s以上还不存在，应该是止损单异常了，平掉当前止盈单
			var (
				//		binanceOrderRes2 *binanceOrder
				binanceOrderRes3 *binanceOrder
				//		orderInfoRes2    *orderInfo
				orderInfoRes3 *orderInfo
				err           error
			)

			binanceOrderRes3, orderInfoRes3, err = requestBinanceDeleteOrder(tmpOrders2.orderInfo.Symbol, tmpOrders2.orderId, tmpOrders2.user.ApiKey, tmpOrders2.user.ApiSecret)
			if nil != err || binanceOrderRes3.OrderId <= 0 {
				fmt.Println("任务，撤销下单，止损，信息：", err, binanceOrderRes3, orderInfoRes3, tmpOrders2.orderInfo)
			}

			// 无论如何清除lastOrder2
			unlockLastOrder2 = append(unlockLastOrder2, k.(string))
			return true
		}

		return true
	})
	for _, vUnlock := range unlockLastOrder2 {
		fmt.Println("删除lastOrder2：", vUnlock)
		lastOrders2.Set(vUnlock, nil)
	}

	// 解锁
	unlock := make([]string, 0)
	last.Iterator(func(k interface{}, v interface{}) bool {
		if nil != v && v.(*lastStruct).lock { // 空
			// 10s以内，先不做判断
			currentTmp := time.Now()
			if math.Abs(currentTmp.Sub(v.(*lastStruct).time).Seconds()) <= 10 {
				return true
			}

			// 10s以后，无止盈止损，做解锁
			if nil == lastOrders.Get(k) && nil == lastOrders2.Get(k) {
				unlock = append(unlock, k.(string))
			}
		}

		return true
	})
	for _, vUnlock := range unlock {
		fmt.Println("解锁：", vUnlock)
		last.Set(vUnlock, nil)
	}

	return true
}

// pullAndSetHandle 拉取的binance数据细节
func (s *sBinanceTraderHistory) pullAndSetHandle(ctx context.Context, traderNum uint64, CountPage int, ipOrderAsc bool, ipMapNeedWait map[string]bool) (resData []*entity.NewBinanceTradeHistory, err error) {
	var (
		PerPullPerPageCountLimitMax = 50 // 每次并行拉取每页最大条数
	)

	if 0 >= s.ips.Size() {
		fmt.Println("ip池子不足，目前数量：", s.ips.Size())
		return nil, err
	}

	// 定义协程共享数据
	dataMap := gmap.New(true) // 结果map，key表示页数，并发安全
	defer dataMap.Clear()
	ipsQueue := gqueue.New() // ip通道，首次使用只用一次
	defer ipsQueue.Close()
	ipsQueueNeedWait := gqueue.New() // ip通道需要等待的
	defer ipsQueueNeedWait.Close()

	// 这里注意一定是key是从0开始到size-1的
	if ipOrderAsc {
		for i := 0; i < s.ips.Size(); i++ {
			if _, ok := ipMapNeedWait[s.ips.Get(i)]; ok {
				if 0 < len(s.ips.Get(i)) { // 1页对应的代理ip的map的key是0
					ipsQueueNeedWait.Push(s.ips.Get(i))
					//if 3949214983441029120 == traderNum {
					//	fmt.Println("暂时别用的ip", s.ips.Get(i))
					//}
				}
			} else {
				if 0 < len(s.ips.Get(i)) { // 1页对应的代理ip的map的key是0
					ipsQueue.Push(s.ips.Get(i))
				}
			}
		}
	} else {
		for i := s.ips.Size() - 1; i >= 0; i-- {
			if _, ok := ipMapNeedWait[s.ips.Get(i)]; ok {
				if 0 < len(s.ips.Get(i)) { // 1页对应的代理ip的map的key是0
					ipsQueueNeedWait.Push(s.ips.Get(i))
					//if 3949214983441029120 == traderNum {
					//	fmt.Println("暂时别用的ip", s.ips.Get(i))
					//}
				}
			} else {
				if 0 < len(s.ips.Get(i)) { // 1页对应的代理ip的map的key是0
					ipsQueue.Push(s.ips.Get(i))
				}
			}
		}
	}

	wg := sync.WaitGroup{}
	for i := 1; i <= CountPage; i++ {
		tmpI := i // go1.22以前的循环陷阱

		wg.Add(1)
		err = s.pool.Add(ctx, func(ctx context.Context) {
			defer wg.Done()

			var (
				retry               = false
				retryTimes          = 0
				retryTimesLimit     = 5 // 重试次数
				successPull         bool
				binanceTradeHistory []*binanceTradeHistoryDataList
			)

			for retryTimes < retryTimesLimit { // 最大重试
				var tmpProxy string
				if 0 < ipsQueue.Len() { // 有剩余先用剩余比较快，不用等2s
					if v := ipsQueue.Pop(); nil != v {
						tmpProxy = v.(string)
						//if 3949214983441029120 == traderNum {
						//	fmt.Println("直接开始", tmpProxy)
						//}
					}
				}

				// 如果没拿到剩余池子
				if 0 >= len(tmpProxy) {
					select {
					case queueItem2 := <-ipsQueueNeedWait.C: // 可用ip，阻塞
						tmpProxy = queueItem2.(string)
						//if 3949214983441029120 == traderNum {
						//	fmt.Println("等待", tmpProxy)
						//}
						time.Sleep(time.Second * 2)
					case <-time.After(time.Minute * 8): // 即使1个ip轮流用，120次查询2秒一次，8分钟超时足够
						fmt.Println("timeout, exit loop")
						break
					}
				}

				// 拿到了代理，执行
				if 0 < len(tmpProxy) {
					binanceTradeHistory, retry, err = s.requestProxyBinanceTradeHistory(tmpProxy, int64(tmpI), int64(PerPullPerPageCountLimitMax), traderNum)
					if nil != err {
						//fmt.Println(err)
					}

					// 使用过的，释放，推入等待queue
					ipsQueueNeedWait.Push(tmpProxy)
				}

				// 需要重试
				if retry {
					//if 3949214983441029120 == traderNum {
					//	fmt.Println("异常需要重试", tmpProxy)
					//}

					retryTimes++
					continue
				}

				// 设置数据
				successPull = true
				dataMap.Set(tmpI, binanceTradeHistory)
				break // 成功直接结束
			}

			// 如果重试次数超过限制且没有成功，存入标记值
			if !successPull {
				dataMap.Set(tmpI, "ERROR")
			}
		})

		if nil != err {
			fmt.Println("添加任务，拉取数据协程异常，错误信息：", err, "交易员：", traderNum)
		}
	}

	// 回收协程
	wg.Wait()

	// 结果，解析处理
	resData = make([]*entity.NewBinanceTradeHistory, 0)
	for i := 1; i <= CountPage; i++ {
		if dataMap.Contains(i) {
			// 从dataMap中获取该页的数据
			dataInterface := dataMap.Get(i)

			// 检查是否是标记值
			if "ERROR" == dataInterface {
				fmt.Println("数据拉取失败，页数：", i, "交易员：", traderNum)
				return nil, err
			}

			// 类型断言，确保dataInterface是我们期望的类型
			if data, ok := dataInterface.([]*binanceTradeHistoryDataList); ok {
				// 现在data是一个binanceTradeHistoryDataList对象数组
				for _, item := range data {
					// 类型处理
					tmpActiveBuy := "false"
					if item.ActiveBuy {
						tmpActiveBuy = "true"
					}

					if "LONG" != item.PositionSide && "SHORT" != item.PositionSide {
						fmt.Println("不识别的仓位，页数：", i, "交易员：", traderNum)
					}

					if "BUY" != item.Side && "SELL" != item.Side {
						fmt.Println("不识别的方向，页数：", i, "交易员：", traderNum)
					}

					resData = append(resData, &entity.NewBinanceTradeHistory{
						Time:                item.Time,
						Symbol:              item.Symbol,
						Side:                item.Side,
						PositionSide:        item.PositionSide,
						Price:               item.Price,
						Fee:                 item.Fee,
						FeeAsset:            item.FeeAsset,
						Quantity:            item.Quantity,
						QuantityAsset:       item.QuantityAsset,
						RealizedProfit:      item.RealizedProfit,
						RealizedProfitAsset: item.RealizedProfitAsset,
						BaseAsset:           item.BaseAsset,
						Qty:                 item.Qty,
						ActiveBuy:           tmpActiveBuy,
					})
				}
			} else {
				fmt.Println("类型断言失败，无法还原数据，页数：", i, "交易员：", traderNum)
				return nil, err
			}
		} else {
			fmt.Println("dataMap不包含，页数：", i, "交易员：", traderNum)
			return nil, err
		}
	}

	return resData, nil
}

// compareBinanceTradeHistoryPageOne 拉取的binance数据细节
func (s *sBinanceTraderHistory) compareBinanceTradeHistoryPageOne(compareMax int64, traderNum uint64, binanceTradeHistoryNewestGroup []*entity.NewBinanceTradeHistory) (ipMapNeedWait map[string]bool, newData []*binanceTradeHistoryDataList, compareResDiff bool, err error) {
	// 试探开始
	var (
		tryLimit            = 3
		binanceTradeHistory []*binanceTradeHistoryDataList
	)

	ipMapNeedWait = make(map[string]bool, 0)
	newData = make([]*binanceTradeHistoryDataList, 0)
	for i := 1; i <= tryLimit; i++ {
		if 0 < s.ips.Size() { // 代理是否不为空
			var (
				ok    = false
				retry = true
			)

			// todo 因为是map，遍历时的第一次，可能一直会用某一条代理信息
			s.ips.Iterator(func(k int, v string) bool {
				ipMapNeedWait[v] = true
				binanceTradeHistory, retry, err = s.requestProxyBinanceTradeHistory(v, 1, compareMax, traderNum)
				if nil != err {
					return true
				}

				if retry {
					return true
				}

				ok = true
				return false
			})

			if ok {
				break
			}
		} else {
			fmt.Println("ip无可用")
			//binanceTradeHistory, err = s.requestBinanceTradeHistory(1, compareMax, traderNum)
			//if nil != err {
			//	return false, err
			//}
			return ipMapNeedWait, newData, false, nil
		}
	}

	// 对比
	if len(binanceTradeHistory) != len(binanceTradeHistoryNewestGroup) {
		fmt.Println("无法对比，条数不同", len(binanceTradeHistory), len(binanceTradeHistoryNewestGroup))
		return ipMapNeedWait, newData, false, nil
	}
	for k, vBinanceTradeHistory := range binanceTradeHistory {
		newData = append(newData, vBinanceTradeHistory)
		if vBinanceTradeHistory.Time == binanceTradeHistoryNewestGroup[k].Time &&
			vBinanceTradeHistory.Symbol == binanceTradeHistoryNewestGroup[k].Symbol &&
			vBinanceTradeHistory.Side == binanceTradeHistoryNewestGroup[k].Side &&
			vBinanceTradeHistory.PositionSide == binanceTradeHistoryNewestGroup[k].PositionSide &&
			IsEqual(vBinanceTradeHistory.Qty, binanceTradeHistoryNewestGroup[k].Qty) && // 数量
			IsEqual(vBinanceTradeHistory.Price, binanceTradeHistoryNewestGroup[k].Price) && //价格
			IsEqual(vBinanceTradeHistory.RealizedProfit, binanceTradeHistoryNewestGroup[k].RealizedProfit) &&
			IsEqual(vBinanceTradeHistory.Quantity, binanceTradeHistoryNewestGroup[k].Quantity) &&
			IsEqual(vBinanceTradeHistory.Fee, binanceTradeHistoryNewestGroup[k].Fee) {
		} else {
			compareResDiff = true
			break
		}
	}

	return ipMapNeedWait, newData, compareResDiff, err
}

// PullAndClose 拉取binance数据
func (s *sBinanceTraderHistory) PullAndClose(ctx context.Context) {
	//var (
	//	err error
	//)
}

// ListenThenOrder 监听拉取的binance数据
func (s *sBinanceTraderHistory) ListenThenOrder(ctx context.Context) {
	// 消费者，不停读取队列数据并输出到终端
	var (
		err error
	)
	consumerPool := grpool.New()
	for {
		var (
			dataInterface interface{}
			data          []*binanceTrade
			ok            bool
		)
		if dataInterface = s.orderQueue.Pop(); dataInterface == nil {
			continue
		}

		if data, ok = dataInterface.([]*binanceTrade); !ok {
			// 处理协程
			fmt.Println("监听程序，解析队列数据错误：", dataInterface)
			continue
		}

		// 处理协程
		err = consumerPool.Add(ctx, func(ctx context.Context) {
			// 初始化一个
			order := make([]*Order, 0)
			order = append(order, &Order{
				Uid:       0,
				BaseMoney: "",
				Data:      make([]*Data, 0),
				InitOrder: 0,
				Rate:      "",
				TraderNum: 0,
			})

			// 同一个交易员的
			for _, v := range data {
				order[0].TraderNum = v.TraderNum
				order[0].Data = append(order[0].Data, &Data{
					Symbol:     v.Symbol,
					Type:       v.Type,
					Price:      v.Price,
					Side:       v.Side,
					Qty:        strconv.FormatFloat(v.QtyFloat, 'f', -1, 64),
					Proportion: "",
					Position:   v.Position,
				})

				fmt.Println(Data{
					Symbol:     v.Symbol,
					Type:       v.Type,
					Price:      v.Price,
					Side:       v.Side,
					Qty:        strconv.FormatFloat(v.QtyFloat, 'f', -1, 64),
					Proportion: "",
					Position:   v.Position,
				})
			}

			if 0 >= len(order[0].Data) {
				return
			}

			// 请求下单
			var res string
			res, err = s.requestSystemOrder(order)
			if "ok" != res {
				fmt.Println("请求下单错,结果信息：", res, err)
				for _, vData := range order[0].Data {
					fmt.Println("请求下单错误，订单信息：", vData)
				}
			}
		})

		if nil != err {
			fmt.Println(err)
		}
	}
}

type binanceTradeHistoryResp struct {
	Data *binanceTradeHistoryData
}

type binanceTradeHistoryData struct {
	Total uint64
	List  []*binanceTradeHistoryDataList
}

type binanceTradeHistoryDataList struct {
	Time                uint64
	Symbol              string
	Side                string
	Price               float64
	Fee                 float64
	FeeAsset            string
	Quantity            float64
	QuantityAsset       string
	RealizedProfit      float64
	RealizedProfitAsset string
	BaseAsset           string
	Qty                 float64
	PositionSide        string
	ActiveBuy           bool
}

type binancePositionResp struct {
	Data []*binancePositionDataList
}

type binancePositionDataList struct {
	Symbol         string
	PositionSide   string
	PositionAmount string
	MarkPrice      string
}

type binancePositionHistoryResp struct {
	Data *binancePositionHistoryData
}

type binancePositionHistoryData struct {
	Total uint64
	List  []*binancePositionHistoryDataList
}

type binancePositionHistoryDataList struct {
	Time   uint64
	Symbol string
	Side   string
	Opened uint64
	Closed uint64
	Status string
}

type binanceTrade struct {
	TraderNum uint64
	Time      uint64
	Symbol    string
	Type      string
	Position  string
	Side      string
	Price     string
	Qty       string
	QtyFloat  float64
}

type Data struct {
	Symbol     string `json:"symbol"`
	Type       string `json:"type"`
	Price      string `json:"price"`
	Side       string `json:"side"`
	Qty        string `json:"qty"`
	Proportion string `json:"proportion"`
	Position   string `json:"position"`
}

type Order struct {
	Uid       uint64  `json:"uid"`
	BaseMoney string  `json:"base_money"`
	Data      []*Data `json:"data"`
	InitOrder uint64  `json:"init_order"`
	Rate      string  `json:"rate"`
	TraderNum uint64  `json:"trader_num"`
}

type SendBody struct {
	Orders    []*Order `json:"orders"`
	InitOrder uint64   `json:"init_order"`
}

type ListenTraderAndUserOrderRequest struct {
	SendBody SendBody `json:"send_body"`
}

type RequestResp struct {
	Status string
}

// 请求下单接口
func (s *sBinanceTraderHistory) requestSystemOrder(Orders []*Order) (string, error) {
	var (
		resp   *http.Response
		b      []byte
		err    error
		apiUrl = "http://127.0.0.1:8125/api/binanceexchange_user/listen_trader_and_user_order_new"
	)

	// 构造请求数据
	requestBody := ListenTraderAndUserOrderRequest{
		SendBody: SendBody{
			Orders: Orders,
		},
	}

	// 序列化为JSON
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		return "", err
	}

	// 创建http.Client并设置超时时间
	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	// 构造http请求
	req, err := http.NewRequest("POST", apiUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err = client.Do(req)
	if err != nil {
		return "", err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	var r *RequestResp
	err = json.Unmarshal(b, &r)
	if err != nil {
		fmt.Println(err)
		return "", err
	}

	return r.Status, nil
}

// 请求binance的下单历史接口
func (s *sBinanceTraderHistory) requestProxyBinanceTradeHistory(proxyAddr string, pageNumber int64, pageSize int64, portfolioId uint64) ([]*binanceTradeHistoryDataList, bool, error) {
	var (
		resp   *http.Response
		res    []*binanceTradeHistoryDataList
		b      []byte
		err    error
		apiUrl = "https://www.binance.com/bapi/futures/v1/friendly/future/copy-trade/lead-portfolio/trade-history"
	)

	proxy, err := url.Parse(proxyAddr)
	if err != nil {
		fmt.Println(err)
		return nil, true, err
	}
	netTransport := &http.Transport{
		Proxy:                 http.ProxyURL(proxy),
		MaxIdleConnsPerHost:   10,
		ResponseHeaderTimeout: time.Second * time.Duration(5),
	}
	httpClient := &http.Client{
		Timeout:   time.Second * 10,
		Transport: netTransport,
	}

	// 构造请求
	contentType := "application/json"
	data := `{"pageNumber":` + strconv.FormatInt(pageNumber, 10) + `,"pageSize":` + strconv.FormatInt(pageSize, 10) + `,portfolioId:` + strconv.FormatUint(portfolioId, 10) + `}`
	resp, err = httpClient.Post(apiUrl, contentType, strings.NewReader(data))
	if err != nil {
		fmt.Println(333, err)
		return nil, true, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(222, err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(111, err)
		return nil, true, err
	}

	var l *binanceTradeHistoryResp
	err = json.Unmarshal(b, &l)
	if err != nil {
		return nil, true, err
	}

	if nil == l.Data {
		return res, true, nil
	}

	res = make([]*binanceTradeHistoryDataList, 0)
	if nil == l.Data.List {
		return res, false, nil
	}

	res = make([]*binanceTradeHistoryDataList, 0)
	for _, v := range l.Data.List {
		res = append(res, v)
	}

	return res, false, nil
}

// 请求binance的仓位历史接口
func (s *sBinanceTraderHistory) requestProxyBinancePositionHistory(proxyAddr string, pageNumber int64, pageSize int64, portfolioId uint64) ([]*binancePositionHistoryDataList, bool, error) {
	var (
		resp   *http.Response
		res    []*binancePositionHistoryDataList
		b      []byte
		err    error
		apiUrl = "https://www.binance.com/bapi/futures/v1/friendly/future/copy-trade/lead-portfolio/position-history"
	)

	proxy, err := url.Parse(proxyAddr)
	if err != nil {
		fmt.Println(err)
		return nil, true, err
	}
	netTransport := &http.Transport{
		Proxy:                 http.ProxyURL(proxy),
		MaxIdleConnsPerHost:   10,
		ResponseHeaderTimeout: time.Second * time.Duration(5),
	}
	httpClient := &http.Client{
		Timeout:   time.Second * 10,
		Transport: netTransport,
	}

	// 构造请求
	contentType := "application/json"
	data := `{"sort":"OPENING","pageNumber":` + strconv.FormatInt(pageNumber, 10) + `,"pageSize":` + strconv.FormatInt(pageSize, 10) + `,portfolioId:` + strconv.FormatUint(portfolioId, 10) + `}`
	resp, err = httpClient.Post(apiUrl, contentType, strings.NewReader(data))
	if err != nil {
		fmt.Println(333, err)
		return nil, true, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(222, err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(111, err)
		return nil, true, err
	}

	var l *binancePositionHistoryResp
	err = json.Unmarshal(b, &l)
	if err != nil {
		return nil, true, err
	}

	if nil == l.Data {
		return res, true, nil
	}

	res = make([]*binancePositionHistoryDataList, 0)
	if nil == l.Data.List {
		return res, false, nil
	}

	res = make([]*binancePositionHistoryDataList, 0)
	for _, v := range l.Data.List {
		res = append(res, v)
	}

	return res, false, nil
}

// 请求binance的持有仓位历史接口，新
func (s *sBinanceTraderHistory) requestProxyBinancePositionHistoryNew(proxyAddr string, portfolioId uint64, cookie string, token string) ([]*binancePositionDataList, bool, error) {
	var (
		resp   *http.Response
		res    []*binancePositionDataList
		b      []byte
		err    error
		apiUrl = "https://www.binance.com/bapi/futures/v1/friendly/future/copy-trade/lead-data/positions?portfolioId=" + strconv.FormatUint(portfolioId, 10)
	)

	proxy, err := url.Parse(proxyAddr)
	if err != nil {
		fmt.Println(err)
		return nil, true, err
	}
	netTransport := &http.Transport{
		Proxy:                 http.ProxyURL(proxy),
		MaxIdleConnsPerHost:   10,
		ResponseHeaderTimeout: time.Second * time.Duration(5),
	}
	httpClient := &http.Client{
		Timeout:   time.Second * 2,
		Transport: netTransport,
	}

	// 构造请求
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		fmt.Println(444444, err)
		return nil, true, err
	}

	// 添加头信息
	req.Header.Set("Clienttype", "web")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Csrftoken", token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0")

	// 构造请求
	resp, err = httpClient.Do(req)
	if err != nil {
		fmt.Println("Error making request:", err)
		fmt.Println(444444, err)
		return nil, true, err
	}

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(4444, err)
		return nil, true, err
	}

	var l *binancePositionResp
	err = json.Unmarshal(b, &l)
	if err != nil {
		return nil, true, err
	}

	if nil == l.Data {
		return res, true, nil
	}

	res = make([]*binancePositionDataList, 0)
	for _, v := range l.Data {
		res = append(res, v)
	}

	return res, false, nil
}

// 请求binance的持有仓位历史接口，新
func (s *sBinanceTraderHistory) requestBinancePositionHistoryNew(portfolioId uint64, cookie string, token string) ([]*binancePositionDataList, bool, error) {
	var (
		resp   *http.Response
		res    []*binancePositionDataList
		b      []byte
		err    error
		apiUrl = "https://www.binance.com/bapi/futures/v1/friendly/future/copy-trade/lead-data/positions?portfolioId=" + strconv.FormatUint(portfolioId, 10)
	)

	// 创建不验证 SSL 证书的 HTTP 客户端
	httpClient := &http.Client{
		Timeout: time.Second * 2,
	}

	// 构造请求
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return nil, true, err
	}

	// 添加头信息
	req.Header.Set("Clienttype", "web")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Csrftoken", token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0")

	// 发送请求
	resp, err = httpClient.Do(req)
	if err != nil {
		fmt.Println("Error making request:", err)
		return nil, true, err
	}

	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(44444, err)
		}
	}(resp.Body)

	// 结果
	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(4444, err)
		return nil, true, err
	}

	//fmt.Println(string(b))
	var l *binancePositionResp
	err = json.Unmarshal(b, &l)
	if err != nil {
		return nil, true, err
	}

	if nil == l.Data {
		return res, true, nil
	}

	res = make([]*binancePositionDataList, 0)
	for _, v := range l.Data {
		res = append(res, v)
	}

	return res, false, nil
}

// 暂时弃用
func (s *sBinanceTraderHistory) requestOrder(pageNumber int64, pageSize int64, portfolioId uint64) ([]*binanceTradeHistoryDataList, error) {
	var (
		resp   *http.Response
		res    []*binanceTradeHistoryDataList
		b      []byte
		err    error
		apiUrl = "https://www.binance.com/bapi/futures/v1/friendly/future/copy-trade/lead-portfolio/trade-history"
	)

	// 构造请求
	contentType := "application/json"
	data := `{"pageNumber":` + strconv.FormatInt(pageNumber, 10) + `,"pageSize":` + strconv.FormatInt(pageSize, 10) + `,portfolioId:` + strconv.FormatUint(portfolioId, 10) + `}`
	resp, err = http.Post(apiUrl, contentType, strings.NewReader(data))
	if err != nil {
		return nil, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	//fmt.Println(string(b))
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	var l *binanceTradeHistoryResp
	err = json.Unmarshal(b, &l)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	if nil == l.Data {
		return res, nil
	}

	if nil == l.Data.List {
		return res, nil
	}

	res = make([]*binanceTradeHistoryDataList, 0)
	for _, v := range l.Data.List {
		res = append(res, v)
	}

	return res, nil
}

// 暂时弃用
func (s *sBinanceTraderHistory) requestBinanceTradeHistory(pageNumber int64, pageSize int64, portfolioId uint64) ([]*binanceTradeHistoryDataList, error) {
	var (
		resp   *http.Response
		res    []*binanceTradeHistoryDataList
		b      []byte
		err    error
		apiUrl = "https://www.binance.com/bapi/futures/v1/friendly/future/copy-trade/lead-portfolio/trade-history"
	)

	// 构造请求
	contentType := "application/json"
	data := `{"pageNumber":` + strconv.FormatInt(pageNumber, 10) + `,"pageSize":` + strconv.FormatInt(pageSize, 10) + `,portfolioId:` + strconv.FormatUint(portfolioId, 10) + `}`
	resp, err = http.Post(apiUrl, contentType, strings.NewReader(data))
	if err != nil {
		return nil, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	//fmt.Println(string(b))
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	var l *binanceTradeHistoryResp
	err = json.Unmarshal(b, &l)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	if nil == l.Data {
		return res, nil
	}

	if nil == l.Data.List {
		return res, nil
	}

	res = make([]*binanceTradeHistoryDataList, 0)
	for _, v := range l.Data.List {
		res = append(res, v)
	}

	return res, nil
}

type binanceOrder struct {
	OrderId       int64
	ExecutedQty   string
	ClientOrderId string
	Symbol        string
	AvgPrice      string
	CumQuote      string
	Side          string
	PositionSide  string
	ClosePosition bool
	Type          string
	Status        string
}

type orderInfo struct {
	Code int64
	Msg  string
}

func requestBinanceOrder(symbol string, side string, orderType string, positionSide string, quantity string, apiKey string, secretKey string) (*binanceOrder, *orderInfo, error) {
	var (
		client       *http.Client
		req          *http.Request
		resp         *http.Response
		res          *binanceOrder
		resOrderInfo *orderInfo
		data         string
		b            []byte
		err          error
		apiUrl       = "https://fapi.binance.com/fapi/v1/order"
	)

	//fmt.Println(symbol, side, orderType, positionSide, quantity, apiKey, secretKey)
	// 时间
	now := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	// 拼请求数据
	data = "symbol=" + symbol + "&side=" + side + "&type=" + orderType + "&positionSide=" + positionSide + "&newOrderRespType=" + "RESULT" + "&quantity=" + quantity + "&timestamp=" + now

	// 加密
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(data))
	signature := hex.EncodeToString(h.Sum(nil))
	// 构造请求

	req, err = http.NewRequest("POST", apiUrl, strings.NewReader(data+"&signature="+signature))
	if err != nil {
		return nil, nil, err
	}
	// 添加头信息
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", apiKey)

	// 请求执行
	client = &http.Client{Timeout: 3 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(string(b), err)
		return nil, nil, err
	}

	var o binanceOrder
	err = json.Unmarshal(b, &o)
	if err != nil {
		fmt.Println(string(b), err)
		return nil, nil, err
	}

	res = &binanceOrder{
		OrderId:       o.OrderId,
		ExecutedQty:   o.ExecutedQty,
		ClientOrderId: o.ClientOrderId,
		Symbol:        o.Symbol,
		AvgPrice:      o.AvgPrice,
		CumQuote:      o.CumQuote,
		Side:          o.Side,
		PositionSide:  o.PositionSide,
		ClosePosition: o.ClosePosition,
		Type:          o.Type,
	}

	if 0 >= res.OrderId {
		//fmt.Println(string(b))
		err = json.Unmarshal(b, &resOrderInfo)
		if err != nil {
			fmt.Println(string(b), err)
			return nil, nil, err
		}
	}

	return res, resOrderInfo, nil
}

func requestBinanceOrderStop(symbol string, side string, positionSide string, quantity string, stopPrice string, price string, apiKey string, secretKey string) (*binanceOrder, *orderInfo, error) {
	//fmt.Println(symbol, side, positionSide, quantity, stopPrice, price, apiKey, secretKey)
	var (
		client       *http.Client
		req          *http.Request
		resp         *http.Response
		res          *binanceOrder
		resOrderInfo *orderInfo
		data         string
		b            []byte
		err          error
		apiUrl       = "https://fapi.binance.com/fapi/v1/order"
	)

	// 时间
	now := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	// 拼请求数据
	data = "symbol=" + symbol + "&side=" + side + "&type=STOP_MARKET&stopPrice=" + stopPrice + "&positionSide=" + positionSide + "&newOrderRespType=" + "RESULT" + "&quantity=" + quantity + "&timestamp=" + now

	// 加密
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(data))
	signature := hex.EncodeToString(h.Sum(nil))
	// 构造请求

	req, err = http.NewRequest("POST", apiUrl, strings.NewReader(data+"&signature="+signature))
	if err != nil {
		return nil, nil, err
	}
	// 添加头信息
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", apiKey)

	// 请求执行
	client = &http.Client{Timeout: 3 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(string(b), err)
		return nil, nil, err
	}

	var o binanceOrder
	err = json.Unmarshal(b, &o)
	if err != nil {
		fmt.Println(string(b), err)
		return nil, nil, err
	}

	res = &binanceOrder{
		OrderId:       o.OrderId,
		ExecutedQty:   o.ExecutedQty,
		ClientOrderId: o.ClientOrderId,
		Symbol:        o.Symbol,
		AvgPrice:      o.AvgPrice,
		CumQuote:      o.CumQuote,
		Side:          o.Side,
		PositionSide:  o.PositionSide,
		ClosePosition: o.ClosePosition,
		Type:          o.Type,
	}

	if 0 >= res.OrderId {
		//fmt.Println(string(b))
		err = json.Unmarshal(b, &resOrderInfo)
		if err != nil {
			fmt.Println(string(b), err)
			return nil, nil, err
		}
	}

	return res, resOrderInfo, nil
}

func requestBinanceOrderStopTakeProfit(symbol string, side string, positionSide string, quantity string, stopPrice string, price string, apiKey string, secretKey string) (*binanceOrder, *orderInfo, error) {
	//fmt.Println(symbol, side, positionSide, quantity, stopPrice, price, apiKey, secretKey)
	var (
		client       *http.Client
		req          *http.Request
		resp         *http.Response
		res          *binanceOrder
		resOrderInfo *orderInfo
		data         string
		b            []byte
		err          error
		apiUrl       = "https://fapi.binance.com/fapi/v1/order"
	)

	// 时间
	now := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	// 拼请求数据
	data = "symbol=" + symbol + "&side=" + side + "&type=TAKE_PROFIT&stopPrice=" + stopPrice + "&price=" + price + "&positionSide=" + positionSide + "&newOrderRespType=" + "RESULT" + "&quantity=" + quantity + "&timestamp=" + now

	// 加密
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(data))
	signature := hex.EncodeToString(h.Sum(nil))
	// 构造请求

	req, err = http.NewRequest("POST", apiUrl, strings.NewReader(data+"&signature="+signature))
	if err != nil {
		return nil, nil, err
	}
	// 添加头信息
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", apiKey)

	// 请求执行
	client = &http.Client{Timeout: 3 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(string(b), err)
		return nil, nil, err
	}

	var o binanceOrder
	err = json.Unmarshal(b, &o)
	if err != nil {
		fmt.Println(string(b), err)
		return nil, nil, err
	}

	res = &binanceOrder{
		OrderId:       o.OrderId,
		ExecutedQty:   o.ExecutedQty,
		ClientOrderId: o.ClientOrderId,
		Symbol:        o.Symbol,
		AvgPrice:      o.AvgPrice,
		CumQuote:      o.CumQuote,
		Side:          o.Side,
		PositionSide:  o.PositionSide,
		ClosePosition: o.ClosePosition,
		Type:          o.Type,
	}

	if 0 >= res.OrderId {
		//fmt.Println(string(b))
		err = json.Unmarshal(b, &resOrderInfo)
		if err != nil {
			fmt.Println(string(b), err)
			return nil, nil, err
		}
	}

	return res, resOrderInfo, nil
}

type BinanceTraderDetailResp struct {
	Data *BinanceTraderDetailData
}

type BinanceTraderDetailData struct {
	MarginBalance string
}

// 拉取交易员交易历史
func requestBinanceTraderDetail(portfolioId uint64) (string, error) {
	var (
		resp   *http.Response
		res    string
		b      []byte
		err    error
		apiUrl = "https://www.binance.com/bapi/futures/v1/friendly/future/copy-trade/lead-portfolio/detail?portfolioId=" + strconv.FormatUint(portfolioId, 10)
	)

	// 构造请求
	resp, err = http.Get(apiUrl)
	if err != nil {
		return res, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return res, err
	}

	var l *BinanceTraderDetailResp
	err = json.Unmarshal(b, &l)
	if err != nil {
		fmt.Println(err)
		return res, err
	}

	if nil == l.Data {
		return res, nil
	}

	return l.Data.MarginBalance, nil
}

type BinanceTraderExchangeInfoResp struct {
	Symbols []*BinanceExchangeInfoSymbol
}

type BinanceExchangeInfoSymbol struct {
	Symbol  string
	Filters []*BinanceExchangeInfoSymbolFilter
}

type BinanceExchangeInfoSymbolFilter struct {
	TickSize   string
	FilterType string
}

// 拉取币种信息
func requestBinanceExchangeInfo() ([]*BinanceExchangeInfoSymbol, error) {
	var (
		resp   *http.Response
		res    []*BinanceExchangeInfoSymbol
		b      []byte
		err    error
		apiUrl = "https://fapi.binance.com/fapi/v1/exchangeInfo"
	)

	// 构造请求
	resp, err = http.Get(apiUrl)
	if err != nil {
		return res, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return res, err
	}

	var l *BinanceTraderExchangeInfoResp
	err = json.Unmarshal(b, &l)
	if err != nil {
		fmt.Println(err)
		return res, err
	}

	if nil == l.Symbols || 0 >= len(l.Symbols) {
		return res, nil
	}

	return l.Symbols, nil
}

// 撤销订单信息
func requestBinanceDeleteOrder(symbol string, orderId string, apiKey string, secretKey string) (*binanceOrder, *orderInfo, error) {
	var (
		client       *http.Client
		req          *http.Request
		resp         *http.Response
		res          *binanceOrder
		resOrderInfo *orderInfo
		data         string
		b            []byte
		err          error
		apiUrl       = "https://fapi.binance.com/fapi/v1/order"
	)

	// 时间
	now := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	// 拼请求数据
	data = "symbol=" + symbol + "&orderId=" + orderId + "&timestamp=" + now

	// 加密
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(data))
	signature := hex.EncodeToString(h.Sum(nil))
	// 构造请求

	req, err = http.NewRequest("DELETE", apiUrl, strings.NewReader(data+"&signature="+signature))
	if err != nil {
		return nil, nil, err
	}
	// 添加头信息
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", apiKey)

	// 请求执行
	client = &http.Client{Timeout: 3 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(string(b), err)
		return nil, nil, err
	}

	var o binanceOrder
	err = json.Unmarshal(b, &o)
	if err != nil {
		fmt.Println(string(b), err)
		return nil, nil, err
	}

	res = &binanceOrder{
		OrderId:       o.OrderId,
		ExecutedQty:   o.ExecutedQty,
		ClientOrderId: o.ClientOrderId,
		Symbol:        o.Symbol,
		AvgPrice:      o.AvgPrice,
		CumQuote:      o.CumQuote,
		Side:          o.Side,
		PositionSide:  o.PositionSide,
		ClosePosition: o.ClosePosition,
		Type:          o.Type,
	}

	if 0 >= res.OrderId {
		//fmt.Println(string(b))
		err = json.Unmarshal(b, &resOrderInfo)
		if err != nil {
			fmt.Println(string(b), err)
			return nil, nil, err
		}
	}

	return res, resOrderInfo, nil
}

func requestBinanceOrderInfo(symbol string, orderId string, apiKey string, secretKey string) (*binanceOrder, error) {
	var (
		client *http.Client
		req    *http.Request
		resp   *http.Response
		res    *binanceOrder
		data   string
		b      []byte
		err    error
		apiUrl = "https://fapi.binance.com/fapi/v1/order"
	)

	// 时间
	now := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	// 拼请求数据
	data = "symbol=" + symbol + "&orderId=" + orderId + "&timestamp=" + now
	// 加密
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(data))
	signature := hex.EncodeToString(h.Sum(nil))
	// 构造请求

	req, err = http.NewRequest("GET", apiUrl, strings.NewReader(data+"&signature="+signature))
	if err != nil {
		return nil, err
	}
	// 添加头信息
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", apiKey)

	// 请求执行
	client = &http.Client{Timeout: 3 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}

	// 结果
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			fmt.Println(err)
		}
	}(resp.Body)

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	var o binanceOrder
	err = json.Unmarshal(b, &o)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	res = &binanceOrder{
		OrderId:       o.OrderId,
		ExecutedQty:   o.ExecutedQty,
		ClientOrderId: o.ClientOrderId,
		Symbol:        o.Symbol,
		AvgPrice:      o.AvgPrice,
		CumQuote:      o.CumQuote,
		Side:          o.Side,
		PositionSide:  o.PositionSide,
		ClosePosition: o.ClosePosition,
		Type:          o.Type,
		Status:        o.Status,
	}

	return res, nil
}

// BinanceExchangeInfoResp 结构体表示 Binance 交易对信息的 API 响应
type BinanceExchangeInfoResp struct {
	Symbols []*BinanceSymbolInfo `json:"symbols"`
}

// BinanceSymbolInfo 结构体表示单个交易对的信息
type BinanceSymbolInfo struct {
	Symbol            string `json:"symbol"`
	Pair              string `json:"pair"`
	ContractType      string `json:"contractType"`
	Status            string `json:"status"`
	BaseAsset         string `json:"baseAsset"`
	QuoteAsset        string `json:"quoteAsset"`
	MarginAsset       string `json:"marginAsset"`
	PricePrecision    int    `json:"pricePrecision"`
	QuantityPrecision int    `json:"quantityPrecision"`
}

// 获取 Binance U 本位合约交易对信息
func getBinanceFuturesPairs() ([]*BinanceSymbolInfo, error) {
	apiUrl := "https://fapi.binance.com/fapi/v1/exchangeInfo"

	// 发送 HTTP GET 请求
	resp, err := http.Get(apiUrl)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				log.Println("关闭响应体错误：", err)
			}
		}
	}()

	// 读取响应体
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析 JSON 响应
	var exchangeInfo *BinanceExchangeInfoResp
	err = json.Unmarshal(body, &exchangeInfo)
	if err != nil {
		return nil, err
	}

	return exchangeInfo.Symbols, nil
}

// GetGateContract 获取合约账号信息
func getGateContract() ([]gateapi.Contract, error) {
	client := gateapi.NewAPIClient(gateapi.NewConfiguration())
	// uncomment the next line if your are testing against testnet
	// client.ChangeBasePath("https://fx-api-testnet.gateio.ws/api/v4")
	ctx := context.WithValue(context.Background(),
		gateapi.ContextGateAPIV4,
		gateapi.GateAPIV4{},
	)

	result, _, err := client.FuturesApi.ListFuturesContracts(ctx, "usdt", &gateapi.ListFuturesContractsOpts{})
	if err != nil {
		var e gateapi.GateAPIError
		if errors.As(err, &e) {
			log.Println("gate api error: ", e.Error())
			return result, err
		}
	}

	return result, nil
}

// PlaceOrderGate places an order on the Gate.io API with dynamic parameters
func placeOrderGate(apiK, apiS, contract string, size int64, reduceOnly bool, autoSize string) (gateapi.FuturesOrder, error) {
	client := gateapi.NewAPIClient(gateapi.NewConfiguration())
	// uncomment the next line if your are testing against testnet
	// client.ChangeBasePath("https://fx-api-testnet.gateio.ws/api/v4")
	ctx := context.WithValue(context.Background(),
		gateapi.ContextGateAPIV4,
		gateapi.GateAPIV4{
			Key:    apiK,
			Secret: apiS,
		},
	)

	order := gateapi.FuturesOrder{
		Contract: contract,
		Size:     size,
		Tif:      "ioc",
		Price:    "0",
	}

	if autoSize != "" {
		order.AutoSize = autoSize
	}

	// 如果 reduceOnly 为 true，添加到请求数据中
	if reduceOnly {
		order.ReduceOnly = reduceOnly
	}

	result, _, err := client.FuturesApi.CreateFuturesOrder(ctx, "usdt", order)

	if err != nil {
		var e gateapi.GateAPIError
		if errors.As(err, &e) {
			log.Println("gate api error: ", e.Error())
			return result, err
		}
	}

	return result, nil
}

// PlaceOrderGate places an order on the Gate.io API with dynamic parameters
func placeLimitCloseOrderGate(apiK, apiS, contract string, price string, autoSize string) (gateapi.FuturesOrder, error) {
	client := gateapi.NewAPIClient(gateapi.NewConfiguration())
	// uncomment the next line if your are testing against testnet
	// client.ChangeBasePath("https://fx-api-testnet.gateio.ws/api/v4")
	ctx := context.WithValue(context.Background(),
		gateapi.ContextGateAPIV4,
		gateapi.GateAPIV4{
			Key:    apiK,
			Secret: apiS,
		},
	)

	order := gateapi.FuturesOrder{
		Contract:     contract,
		Size:         0,
		Price:        price,
		Tif:          "gtc",
		ReduceOnly:   true,
		AutoSize:     autoSize,
		IsReduceOnly: true,
		IsClose:      true,
	}

	result, _, err := client.FuturesApi.CreateFuturesOrder(ctx, "usdt", order)

	if err != nil {
		var e gateapi.GateAPIError
		if errors.As(err, &e) {
			log.Println("gate api error: ", e.Error())
			return result, err
		}
	}

	return result, nil
}

// PlaceOrderGate places an order on the Gate.io API with dynamic parameters
func removeLimitCloseOrderGate(apiK, apiS, orderId string) (gateapi.FuturesOrder, error) {
	client := gateapi.NewAPIClient(gateapi.NewConfiguration())
	// uncomment the next line if your are testing against testnet
	// client.ChangeBasePath("https://fx-api-testnet.gateio.ws/api/v4")
	ctx := context.WithValue(context.Background(),
		gateapi.ContextGateAPIV4,
		gateapi.GateAPIV4{
			Key:    apiK,
			Secret: apiS,
		},
	)

	result, _, err := client.FuturesApi.CancelFuturesOrder(ctx, "usdt", orderId)

	if err != nil {
		var e gateapi.GateAPIError
		if errors.As(err, &e) {
			log.Println("gate api error: ", e.Error())
			return result, err
		}
	}

	return result, nil
}

// PlaceOrderGate places an order on the Gate.io API with dynamic parameters
func getOrderGate(apiK, apiS, orderId string) (gateapi.FuturesOrder, error) {
	client := gateapi.NewAPIClient(gateapi.NewConfiguration())
	// uncomment the next line if your are testing against testnet
	// client.ChangeBasePath("https://fx-api-testnet.gateio.ws/api/v4")
	ctx := context.WithValue(context.Background(),
		gateapi.ContextGateAPIV4,
		gateapi.GateAPIV4{
			Key:    apiK,
			Secret: apiS,
		},
	)

	result, _, err := client.FuturesApi.GetFuturesOrder(ctx, "usdt", orderId)

	if err != nil {
		var e gateapi.GateAPIError
		if errors.As(err, &e) {
			log.Println("gate api error: ", e.Error())
			return result, err
		}
	}

	return result, nil
}

func placeLimitOrderGate(apiK, apiS, contract string, rule, timeLimit int32, price string, autoSize string) (gateapi.TriggerOrderResponse, error) {
	client := gateapi.NewAPIClient(gateapi.NewConfiguration())
	ctx := context.WithValue(context.Background(),
		gateapi.ContextGateAPIV4,
		gateapi.GateAPIV4{
			Key:    apiK,
			Secret: apiS,
		},
	)

	order := gateapi.FuturesPriceTriggeredOrder{
		Initial: gateapi.FuturesInitialOrder{
			Contract:     contract,
			Size:         0,
			Price:        price,
			Tif:          "gtc",
			ReduceOnly:   true,
			AutoSize:     autoSize,
			IsReduceOnly: true,
			IsClose:      true,
		},
		Trigger: gateapi.FuturesPriceTrigger{
			StrategyType: 0,
			PriceType:    0,
			Price:        price,
			Rule:         rule,
			Expiration:   timeLimit,
		},
	}

	result, _, err := client.FuturesApi.CreatePriceTriggeredOrder(ctx, "usdt", order)

	if err != nil {
		var e gateapi.GateAPIError
		if errors.As(err, &e) {
			log.Println("gate api error: ", e.Error())
			return result, err
		}
		return result, err
	}

	return result, nil
}

type KLineMOne struct {
	ID                  int64
	StartTime           int64
	EndTime             int64
	StartPrice          float64
	TopPrice            float64
	LowPrice            float64
	EndPrice            float64
	DealTotalAmount     float64
	DealAmount          float64
	DealTotal           int64
	DealSelfTotalAmount float64
	DealSelfAmount      float64
}

type KLineDay struct {
	OpenTime               int64
	Open, High, Low, Close string
	Volume                 string
	CloseTime              int64
	QuoteAssetVolume       string
	TradeNum               int
	TakerBuyBaseVolume     string
	TakerBuyQuoteVolume    string
	Ignore                 string
}

func requestBinanceDailyKLines(symbol, startTime, endTime string, limit string) ([]*KLineDay, error) {
	apiUrl := "https://api.binance.com/api/v3/klines"

	// 参数
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", "4h") // 日线
	if startTime != "" {
		params.Set("startTime", startTime)
	}
	if endTime != "" {
		params.Set("endTime", endTime)
	}
	if limit != "" {
		params.Set("limit", limit)
	}

	// 构建完整URL
	u, err := url.ParseRequestURI(apiUrl)
	if err != nil {
		return nil, err
	}
	u.RawQuery = params.Encode()

	// 请求
	client := http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析
	var rawData [][]interface{}
	err = json.Unmarshal(body, &rawData)
	if err != nil {
		return nil, err
	}

	// 转换成结构体
	var result []*KLineDay
	for _, item := range rawData {
		result = append(result, &KLineDay{
			OpenTime:            int64(item[0].(float64)),
			Open:                item[1].(string),
			High:                item[2].(string),
			Low:                 item[3].(string),
			Close:               item[4].(string),
			Volume:              item[5].(string),
			CloseTime:           int64(item[6].(float64)),
			QuoteAssetVolume:    item[7].(string),
			TradeNum:            int(item[8].(float64)),
			TakerBuyBaseVolume:  item[9].(string),
			TakerBuyQuoteVolume: item[10].(string),
			Ignore:              item[11].(string),
		})
	}

	return result, nil
}

type ExchangeInfo struct {
	Timezone   string   `json:"timezone"`
	ServerTime int64    `json:"serverTime"`
	Symbols    []Symbol `json:"symbols"`
}

type Symbol struct {
	Symbol             string   `json:"symbol"`
	Status             string   `json:"status"`
	BaseAsset          string   `json:"baseAsset"`
	BaseAssetPrecision int      `json:"baseAssetPrecision"`
	QuoteAsset         string   `json:"quoteAsset"`
	QuotePrecision     int      `json:"quotePrecision"`
	OrderTypes         []string `json:"orderTypes"`
	IsSpotTrading      bool     `json:"isSpotTradingAllowed"`
	IsMarginTrading    bool     `json:"isMarginTradingAllowed"`
}

func getBinanceExchangeInfo() (*ExchangeInfo, error) {
	url := "https://api.binance.com/api/v3/exchangeInfo"

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析 JSON
	var info ExchangeInfo
	err = json.Unmarshal(body, &info)
	if err != nil {
		return nil, err
	}

	return &info, nil
}

// 结构体定义
type FuturesPrice struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

// 请求函数
func getUSDMFuturesPrice(symbol string) (*FuturesPrice, error) {
	url := fmt.Sprintf("https://fapi.binance.com/fapi/v1/ticker/price?symbol=%s", symbol)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var price FuturesPrice
	err = json.Unmarshal(body, &price)
	if err != nil {
		return nil, err
	}

	return &price, nil
}

// 获取所有 U 本位合约的当前价格
func getAllUSDMFuturesPrices() (map[string]float64, error) {
	url := "https://fapi.binance.com/fapi/v1/ticker/price"

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var prices []FuturesPrice
	err = json.Unmarshal(body, &prices)
	if err != nil {
		return nil, err
	}

	// 转换为 map：symbol -> price(float64)
	priceMap := make(map[string]float64)
	for _, p := range prices {
		var f float64
		f, err = strconv.ParseFloat(p.Price, 10)
		if err != nil {
			continue
		}

		if 0 >= f {
			fmt.Println("价格0，usdt")
			continue
		}

		priceMap[p.Symbol] = f
	}

	return priceMap, nil
}
