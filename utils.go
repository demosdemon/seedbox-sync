package main

import "github.com/pkg/errors"

func ExactChannel[T any](ch <-chan T, count int) ([]T, error) {
	result := make([]T, 0, count)
	for i := 0; i < count; i++ {
		v, ok := <-ch
		if !ok {
			return result, errors.New("channel closed")
		}
		result = append(result, v)
	}
	return result, nil
}
