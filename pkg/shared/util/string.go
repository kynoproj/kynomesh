/*
Copyright 2026 The Kynoproj Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package util

import (
	"crypto/rand"
	"hash/crc32"
	"math/big"
	"regexp"
	"strings"
)

// RandomString generate a random string with given length
func RandomString(length int) string {
	seeds := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		num, _ := rand.Int(rand.Reader, big.NewInt(int64(len(seeds))))
		result[i] = seeds[num.Int64()]
	}
	return string(result)
}

func RandomLowerCaseString(length int) string {
	return strings.ToLower(RandomString(length))
}

func DNS1035(str string) string {
	re := regexp.MustCompile(`[^a-z0-9-]+`)
	return re.ReplaceAllString(strings.ToLower(str), "-")
}

// Hashcode returns a unique hashcode of a string.
func Hashcode(str string) int {
	i := int(crc32.ChecksumIEEE([]byte(str)))
	if i >= 0 {
		return i
	}
	if -i >= 0 {
		return -i
	}
	return 0
}
