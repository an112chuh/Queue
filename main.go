package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type QueueInfo struct {
	Value, IsChanged int
}

type QueueWithMutex struct {
	mx  sync.Mutex
	Map map[string][]string
}

type LenWithMutex struct {
	mx  sync.Mutex
	Map map[string]int
}

type InfoGlobalWithMutex struct {
	mx  sync.Mutex
	Map map[string]QueueInfo
}

func main() {
	Port := 0
	var err error
	if len(os.Args) == 2 {
		Port, err = strconv.Atoi(os.Args[1])
		if err != nil {
			fmt.Println(err)
			return
		}
	} else {
		fmt.Print("Incorrect number of arguments! Need 1, have ")
		fmt.Println(len(os.Args) - 1)
		return
	}
	chName := make(chan int)
	var Queue QueueWithMutex
	var WaitingLens LenWithMutex
	var InfoGlobal InfoGlobalWithMutex
	Queue.Map = make(map[string][]string)
	WaitingLens.Map = make(map[string]int)
	InfoGlobal.Map = make(map[string]QueueInfo)
	//	InfoLocal := make(map[string]QueueInfo)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		QueueName := r.URL.Path[1:]
		switch r.Method {
		case http.MethodPut:
			NewElement := r.FormValue("v")
			if NewElement == "" {
				http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
				return
			}
			Queue.mx.Lock()
			Queue.Map[QueueName] = append(Queue.Map[QueueName], NewElement)
			Queue.mx.Unlock()
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			Queue.mx.Lock()
			CurrentQueue, ok := Queue.Map[QueueName]
			Queue.mx.Unlock()
			if len(CurrentQueue) > 0 && ok {
				fmt.Fprintf(w, CurrentQueue[0])
				CurrentQueue[0] = ""
				CurrentQueue = CurrentQueue[1:]
				Queue.mx.Lock()
				Queue.Map[QueueName] = CurrentQueue
				Queue.mx.Unlock()
				return
			}
			WaitingLens.mx.Lock()
			_, ok = WaitingLens.Map[QueueName]
			WaitingLens.mx.Unlock()
			if !ok {
				WaitingLens.mx.Lock()
				WaitingLens.Map[QueueName] = 1
				WaitingLens.mx.Unlock()
			} else {
				WaitingLens.mx.Lock()
				WaitingLens.Map[QueueName]++
				WaitingLens.mx.Unlock()
			}
			WaitingLens.mx.Lock()
			CurrentNumOfWaiting := WaitingLens.Map[QueueName]
			WaitingLens.mx.Unlock()
			InfoGlobal.mx.Lock()
			Info, ok := InfoGlobal.Map[QueueName]
			InfoGlobal.mx.Unlock()
			if !ok || CurrentNumOfWaiting == 1 {
				Info.Value = int(^uint(0) >> 1)
				Info.IsChanged = -1
				InfoGlobal.mx.Lock()
				InfoGlobal.Map[QueueName] = Info
				InfoGlobal.mx.Unlock()
			}
			TimeOfTimeOut := r.FormValue("timeout")
			if TimeOfTimeOut == "" {
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				WaitingLens.mx.Lock()
				WaitingLens.Map[QueueName]--
				WaitingLens.mx.Unlock()
				return
			}
			TimeOfTimeOutInt, err := strconv.Atoi(TimeOfTimeOut)
			if err != nil {
				fmt.Println(err)
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				WaitingLens.mx.Lock()
				WaitingLens.Map[QueueName]--
				WaitingLens.mx.Unlock()
				return
			}
			ctx, cancel1 := context.WithTimeout(context.Background(), time.Duration(time.Second*time.Duration(TimeOfTimeOutInt)))
			defer cancel1()
			worker, cancel := context.WithCancel(context.Background())

			go func() {
				var InfoLocal QueueInfo
				InfoLocal.Value = int(^uint(0) >> 1)
				InfoLocal.IsChanged = -1
				p := 0
				for {
					tmpValue := InfoLocal.Value
					tmpIsChanged := InfoLocal.IsChanged
					InfoGlobal.mx.Lock()
					InfoLocal = InfoGlobal.Map[QueueName]
					InfoGlobal.mx.Unlock()
					if tmpValue != InfoLocal.Value || tmpIsChanged != InfoLocal.IsChanged {
						if InfoLocal.Value == CurrentNumOfWaiting {
							if CurrentNumOfWaiting == 1 && WaitingLens.Map[QueueName] == 0 {
								InfoGlobal.mx.Lock()
								Info, ok := InfoGlobal.Map[QueueName]
								InfoGlobal.mx.Unlock()
								if ok {
									Info.Value = int(^uint(0) >> 1)
									InfoGlobal.mx.Lock()
									InfoGlobal.Map[QueueName] = Info
									InfoGlobal.mx.Unlock()
								}
							}
							return
						}
						if CurrentNumOfWaiting > InfoLocal.Value {
							CurrentNumOfWaiting--
						}
					}
					Queue.mx.Lock()
					CurrentQueue, ok := Queue.Map[QueueName]
					Queue.mx.Unlock()
					if ok {
						if len(CurrentQueue) > 0 {
							if CurrentNumOfWaiting == 1 {
								fmt.Fprintf(w, CurrentQueue[0])
								CurrentQueue[0] = ""
								CurrentQueue = CurrentQueue[1:]
								Queue.mx.Lock()
								Queue.Map[QueueName] = CurrentQueue
								Queue.mx.Unlock()
								p = 1
								WaitingLens.mx.Lock()
								WaitingLens.Map[QueueName]--
								tmp := WaitingLens.Map[QueueName]
								WaitingLens.mx.Unlock()
								if tmp >= 1 {
									for i := 0; i < tmp; i++ {
										chName <- 0
									}
								} else {
									InfoGlobal.mx.Lock()
									Info, ok := InfoGlobal.Map[QueueName]
									InfoGlobal.mx.Unlock()
									if ok {
										Info.Value = int(^uint(0) >> 1)
										InfoGlobal.mx.Lock()
										InfoGlobal.Map[QueueName] = Info
										InfoGlobal.mx.Unlock()
									}

								}
							} else {
								CurrentNumOfWaiting--
								<-chName
							}
						}
						if p == 1 {
							cancel()
							break
						}
					}
				}
			}()
			select {
			case <-ctx.Done():
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				InfoGlobal.mx.Lock()
				Info, ok := InfoGlobal.Map[QueueName]
				InfoGlobal.mx.Unlock()
				if ok {
					if InfoGlobal.Map[QueueName].Value == CurrentNumOfWaiting {
						Info.IsChanged *= (-1)
					} else {
						Info.Value = CurrentNumOfWaiting
					}
					InfoGlobal.mx.Lock()
					InfoGlobal.Map[QueueName] = Info
					InfoGlobal.mx.Unlock()
				}
				WaitingLens.mx.Lock()
				WaitingLens.Map[QueueName]--
				WaitingLens.mx.Unlock()
				cancel1()
				return
			case <-worker.Done():
				return
			}
		}

	})
	fmt.Println("STARTED:")
	APP_IP := "127.0.0.1"
	fmt.Println("Server address : " + APP_IP + ":" + strconv.Itoa(Port))
	http.ListenAndServe(APP_IP+":"+strconv.Itoa(Port), nil)
}
