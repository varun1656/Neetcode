type ValTime struct {
	value     string
	timestamp int
}

type TimeMap struct {
	store map[string][]ValTime
}

func Constructor() TimeMap {
	return TimeMap{store: make(map[string][]ValTime)}
}

func (this *TimeMap) Set(key string, value string, timestamp int) {
	if _, exists := this.store[key]; !exists {
		this.store[key] = []ValTime{}
	}
	this.store[key] = append(this.store[key], ValTime{value: value, timestamp: timestamp})
}

func (this *TimeMap) Get(key string, timestamp int) string {
	if _, exists := this.store[key]; !exists {
		return ""
	}
	candidate := math.MinInt
	res := ""
	start, end := 0, len(this.store[key])-1
	for start <= end {
		mid := start + (end-start)/2
		temp := this.store[key][mid].timestamp
		if temp == timestamp {
			return this.store[key][mid].value
		}
		if temp < timestamp {
			if candidate < temp {
				candidate = temp
				res = this.store[key][mid].value
			}
			start = mid + 1
		} else {
			end = mid - 1
		}
	}
	return res
}

/**
 * Your TimeMap object will be instantiated and called as such:
 * obj := Constructor();
 * obj.Set(key,value,timestamp);
 * param_2 := obj.Get(key,timestamp);
 */