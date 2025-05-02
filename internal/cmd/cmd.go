package cmd

import (
	"binance_data_gf/internal/model/entity"
	"binance_data_gf/internal/service"
	"context"
	"fmt"
	"github.com/gogf/gf/v2/os/gcmd"
	"github.com/gogf/gf/v2/os/gtimer"
	"strconv"
	"time"
)

var (
	Main = &gcmd.Command{
		Name: "main",
	}

	// TraderGuiNew 监听系统中指定的交易员-龟兔赛跑
	TraderGuiNew = &gcmd.Command{
		Name:  "traderGuiNew",
		Brief: "listen trader",
		Func: func(ctx context.Context, parser *gcmd.Parser) (err error) {
			serviceBinanceTrader := service.BinanceTraderHistory()

			// 初始化根据数据库现有人
			if !serviceBinanceTrader.UpdateCoinInfo(ctx) {
				fmt.Println("初始化币种失败，fail")
				return nil
			}
			fmt.Println("初始化币种成功，ok")

			// 拉龟兔的保证金
			serviceBinanceTrader.PullAndSetBaseMoneyNewGuiTuAndUser(ctx)

			// 10秒/次，拉取保证金
			handle := func(ctx context.Context) {
				serviceBinanceTrader.PullAndSetBaseMoneyNewGuiTuAndUser(ctx)
			}
			gtimer.AddSingleton(ctx, time.Second*10, handle)

			// 30秒/次，加新人
			handle2 := func(ctx context.Context) {
				serviceBinanceTrader.InsertGlobalUsersNew(ctx)
			}
			gtimer.AddSingleton(ctx, time.Second*30, handle2)

			// 300秒/次，币种信息
			handle3 := func(ctx context.Context) {
				serviceBinanceTrader.UpdateCoinInfo(ctx)
			}
			gtimer.AddSingleton(ctx, time.Second*300, handle3)

			//// 1秒/次，处理订单信息任务流
			//handle4 := func(ctx context.Context) {
			//	serviceBinanceTrader.HandleOrderAndOrder2(ctx)
			//}
			//gtimer.AddSingleton(ctx, time.Second*1, handle4)

			// 任务1 同步订单
			go func() {
				serviceBinanceTrader.PullAndOrderNewGuiTuPlay(ctx)
			}()

			// 阻塞
			select {}
		},
	}
)

// 全局变量来跟踪定时任务
var (
	traderSingleton    = make(map[uint64]*gtimer.Entry)
	traderSingletonNew = make(map[uint64]*gtimer.Entry)
)

func updateTradersPeriodically(ctx context.Context, serviceBinanceTrader service.IBinanceTraderHistory) {
	// 每分钟查询数据库以更新交易员任务
	interval := time.Minute

	for {
		updateTraders(ctx, serviceBinanceTrader)
		time.Sleep(interval)
	}
}

func updateTradersPeriodicallyNew(ctx context.Context, serviceBinanceTrader service.IBinanceTraderHistory) {
	// 每分钟查询数据库以更新交易员任务
	interval := time.Minute

	for {
		updateTradersNew(ctx, serviceBinanceTrader)
		time.Sleep(interval)
	}
}

func updateTraders(ctx context.Context, serviceBinanceTrader service.IBinanceTraderHistory) {
	newTraderIDs, err := fetchTraderIDsFromDB(ctx)
	if err != nil {
		fmt.Println("查询数据库时出错:", err)
		return
	}

	// 空的情况，这里不会做任何修改，那么手动把程序停掉就行了
	if 0 >= len(newTraderIDs) {
		return
	}

	// 不存在新增
	idMap := make(map[uint64]bool, 0)
	for _, vNewTraderIDs := range newTraderIDs {
		idMap[vNewTraderIDs] = true
		if _, ok := traderSingleton[vNewTraderIDs]; !ok { // 不存在新增
			addTraderTask(ctx, vNewTraderIDs, serviceBinanceTrader)
		}
	}

	// 反向检测，不存在删除
	for k, _ := range traderSingleton {
		if _, ok := idMap[k]; !ok {
			removeTraderTask(k)
		}
	}
}

func updateTradersNew(ctx context.Context, serviceBinanceTrader service.IBinanceTraderHistory) {
	newTraderIDs, err := fetchTraderIDsFromDBNew(ctx)
	if err != nil {
		fmt.Println("新，查询数据库时出错:", err)
		return
	}

	// 空的情况，这里不会做任何修改，那么手动把程序停掉就行了
	if 0 >= len(newTraderIDs) {
		return
	}

	// 不存在新增
	idMap := make(map[uint64]bool, 0)
	for k, vNewTraderIDs := range newTraderIDs {
		idMap[vNewTraderIDs] = true
		if _, ok := traderSingletonNew[vNewTraderIDs]; !ok { // 不存在新增
			addTraderTaskNew(ctx, vNewTraderIDs, serviceBinanceTrader, k)
		}
	}

	// 反向检测，不存在删除
	for k, _ := range traderSingletonNew {
		if _, ok := idMap[k]; !ok {
			removeTraderTaskNew(k)
		}
	}
}

func fetchTraderIDsFromDB(ctx context.Context) ([]uint64, error) {
	var (
		err error
	)
	traderNums := make([]uint64, 0)

	traders := make([]*entity.NewBinanceTrader, 0)
	traders, err = service.NewBinanceTrader().GetAllTraders(ctx)
	if nil != err {
		return traderNums, err
	}

	for _, vTraders := range traders {
		traderNums = append(traderNums, vTraders.TraderNum)
	}

	return traderNums, err
}

func fetchTraderIDsFromDBNew(ctx context.Context) ([]uint64, error) {
	var (
		err error
	)
	traderNums := make([]uint64, 0)

	traders := make([]*entity.Trader, 0)
	traders, err = service.Trader().GetAllTraders(ctx)
	if nil != err {
		return traderNums, err
	}

	for _, vTraders := range traders {
		var traderNum uint64
		traderNum, err = strconv.ParseUint(vTraders.PortfolioId, 10, 64)
		if nil != err {
			fmt.Println("新，添加交易员，解析交易员trader_num异常：", vTraders)
			continue
		}

		traderNums = append(traderNums, traderNum)
	}

	return traderNums, err
}

func initIpUpdateTask(ctx context.Context, serviceBinanceTrader service.IBinanceTraderHistory) {
	err := serviceBinanceTrader.UpdateProxyIp(ctx)
	if err != nil {
		fmt.Println("ip更新任务运行时出错:", err)
	}
}

func initListenAndOrderTask(ctx context.Context, serviceBinanceTrader service.IBinanceTraderHistory) {
	serviceBinanceTrader.ListenThenOrder(ctx)
}

func pullAndCloseTask(ctx context.Context, serviceBinanceTrader service.IBinanceTraderHistory) {
	serviceBinanceTrader.PullAndClose(ctx)
}

func addIpUpdateTask(ctx context.Context, serviceBinanceTrader service.IBinanceTraderHistory) {
	// 任务
	handle := func(ctx context.Context) {
		err := serviceBinanceTrader.UpdateProxyIp(ctx)
		if err != nil {
			fmt.Println("ip更新任务运行时出错:", err)
		}
	}

	// 小于ip最大活性时长
	gtimer.AddSingleton(ctx, time.Minute*20, handle)
}

func addTraderTask(ctx context.Context, traderID uint64, serviceBinanceTrader service.IBinanceTraderHistory) {
	// 任务
	handle := func(ctx context.Context) {
		relTraderId := traderID // go1.22以前有循环变量陷阱，不思考这里是否也会如此，直接用临时变量解决
		err := serviceBinanceTrader.PullAndOrder(ctx, relTraderId)
		if err != nil {
			fmt.Println("任务运行时出错:", "交易员信息:", relTraderId, "错误信息:", err)
		}
	}
	traderSingleton[traderID] = gtimer.AddSingleton(ctx, time.Second*2, handle)
	fmt.Println("添加成功交易员:", traderID)
}

func removeTraderTask(traderID uint64) {
	if entry, exists := traderSingleton[traderID]; exists {
		entry.Close()
		delete(traderSingleton, traderID)
		fmt.Println("删除成功交易员:", traderID)
	}
}

func addTraderTaskNew(ctx context.Context, traderID uint64, serviceBinanceTrader service.IBinanceTraderHistory, k int) {
	// 任务
	handle := func(ctx context.Context) {
		relTraderId := traderID // go1.22以前有循环变量陷阱，不思考这里是否也会如此，直接用临时变量解决
		err := serviceBinanceTrader.PullAndOrderNew(ctx, relTraderId, k)
		if err != nil {
			fmt.Println("新，任务运行时出错:", "交易员信息:", relTraderId, "错误信息:", err)
		}
	}

	// 每秒1次
	traderSingletonNew[traderID] = gtimer.AddSingleton(ctx, time.Second, handle)
	fmt.Println("新，添加成功交易员:", traderID)
}

func removeTraderTaskNew(traderID uint64) {
	if entry, exists := traderSingletonNew[traderID]; exists {
		entry.Close()
		delete(traderSingletonNew, traderID)
		fmt.Println("新，删除成功交易员:", traderID)
	}
}
