// Copyright (c) 2023 BVK Chaitanya

package coinbase

import (
	"log/slog"
	"time"

	"github.com/bvkgo/topic/v2"
)

type orderStatus struct {
	status     string
	localTime  time.Time
	serverTime time.Time
}

type orderData struct {
	statusTopic *topic.Topic[*orderStatus]
}

func newOrderData(status string, serverTime time.Time) *orderData {
	d := &orderData{
		statusTopic: topic.New[*orderStatus](),
	}
	d.setStatus(status, serverTime)
	return d
}

func (d *orderData) Close() {
	d.statusTopic.Close()
}

func (d *orderData) status() (string, time.Time) {
	s, _ := topic.Recent(d.statusTopic)
	return s.status, s.localTime
}

func (d *orderData) setStatus(new string, serverTime time.Time) {
	if v, ok := topic.Recent(d.statusTopic); ok {
		if v.status == new {
			return
		}
		if serverTime.Before(v.serverTime) {
			slog.Warn("order status change with past server time is ignored", "cur-status-time", v.serverTime, "new-status-time", serverTime)
			return
		}
	}
	s := &orderStatus{
		status:     new,
		localTime:  time.Now(),
		serverTime: serverTime,
	}
	d.statusTopic.SendCh() <- s
}

// {
//   "channel": "user",
//   "client_id": "",
//   "timestamp": "2023-10-01T14:46:50.56108356Z",
//   "sequence_num": 1,
//   "events": [
//     {
//       "type": "snapshot",
//       "orders": [
//         {
//           "order_id": "1b3a4f9b-f7af-44f5-9be2-5eb86d51b299",
//           "client_order_id": "dd7ab7c4-e748-403a-a95a-9ff8e9aeeaab",
//           "cumulative_quantity": "0",
//           "leaves_quantity": "100",
//           "avg_price": "0",
//           "total_fees": "0",
//           "status": "OPEN",
//           "product_id": "BCH-USD",
//           "creation_time": "2023-07-01T01:12:04.674856Z",
//           "order_side": "SELL",
//           "order_type": "Limit",
//           "cancel_reason": "",
//           "reject_Reason": ""
//         },
//         {
//           "order_id": "bba85c81-c99c-4d02-979c-46d5c757fa88",
//           "client_order_id": "43191fe0-9159-4efd-8f74-a32dade4b6f2",
//           "cumulative_quantity": "0",
//           "leaves_quantity": "1000",
//           "avg_price": "0",
//           "total_fees": "0",
//           "status": "OPEN",
//           "product_id": "BCH-USD",
//           "creation_time": "2023-06-11T15:50:07.67849Z",
//           "order_side": "BUY",
//           "order_type": "Limit",
//           "cancel_reason": "",
//           "reject_Reason": ""
//         },
//         {
//           "order_id": "126f6e67-c085-4101-b13b-da52d402daf0",
//           "client_order_id": "a0cfa389-77e3-4d8f-ad1d-2c532c26a019",
//           "cumulative_quantity": "0",
//           "leaves_quantity": "10",
//           "avg_price": "0",
//           "total_fees": "0",
//           "status": "OPEN",
//           "product_id": "ETH-USD",
//           "creation_time": "2023-03-14T15:17:28.29963Z",
//           "order_side": "BUY",
//           "order_type": "Limit",
//           "cancel_reason": "",
//           "reject_Reason": ""
//         },
//         {
//           "order_id": "f0c38ed5-9957-4b36-9065-27e36eb91ae6",
//           "client_order_id": "13d64bbd-e832-41ab-87d6-71115a2ca5f1",
//           "cumulative_quantity": "0",
//           "leaves_quantity": "249",
//           "avg_price": "0",
//           "total_fees": "0",
//           "status": "OPEN",
//           "product_id": "AVAX-USD",
//           "creation_time": "2023-02-21T07:19:55.49926Z",
//           "order_side": "SELL",
//           "order_type": "Limit",
//           "cancel_reason": "",
//           "reject_Reason": ""
//         },
//         {
//           "order_id": "473a7e63-c02d-45ea-9a13-40df962e1fb3",
//           "client_order_id": "91fae9d6-b62c-4779-ba95-7c7b4e03929b",
//           "cumulative_quantity": "0",
//           "leaves_quantity": "250",
//           "avg_price": "0",
//           "total_fees": "0",
//           "status": "OPEN",
//           "product_id": "AVAX-USD",
//           "creation_time": "2023-02-21T07:18:49.017862Z",
//           "order_side": "SELL",
//           "order_type": "Limit",
//           "cancel_reason": "",
//           "reject_Reason": ""
//         }
//       ]
//     }
//   ]
// }

// {
//   "channel": "user",
//   "client_id": "",
//   "timestamp": "2023-10-02T00:24:17.860313351Z",
//   "sequence_num": 1054,
//   "events": [
//     {
//       "type": "update",
//       "orders": [
//         {
//           "order_id": "01f2c6a2-703c-4328-a11d-0f17adb23a73",
//           "client_order_id": "20e70d44-0cab-4fc3-b592-7a1a459017f4",
//           "cumulative_quantity": "0",
//           "leaves_quantity": "1",
//           "avg_price": "0",
//           "total_fees": "0",
//           "status": "OPEN",
//           "product_id": "BCH-USD",
//           "creation_time": "2023-10-02T00:24:17.700884Z",
//           "order_side": "BUY",
//           "order_type": "Limit",
//           "cancel_reason": "",
//           "reject_Reason": ""
//         }
//       ]
//     }
//   ]
// }
