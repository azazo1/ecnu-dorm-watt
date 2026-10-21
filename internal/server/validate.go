package server

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxNameLength 是 room_no 与 elcbuis 的字节上限.
const maxNameLength = 128

// validateRoomNumber 校验 room_no.
func validateRoomNumber(roomNo string) error {
	if roomNo == "" {
		return validationFailedf("room_no 不能为空")
	}
	if len(roomNo) > maxNameLength {
		return validationFailedf("room_no 不能超过 %d 字节", maxNameLength)
	}
	if roomNo == "." || roomNo == ".." {
		return validationFailedf("room_no 不能是 . 或 ..")
	}
	if strings.ContainsAny(roomNo, `/\`) {
		return validationFailedf("room_no 不能包含斜杠")
	}
	if hasControlOrSpace(roomNo) {
		return validationFailedf("room_no 不能包含控制字符或空白字符")
	}
	return nil
}

// validateBuilding 校验 elcbuis.
func validateBuilding(building string) error {
	if building == "" {
		return validationFailedf("elcbuis 不能为空")
	}
	if len(building) > maxNameLength {
		return validationFailedf("elcbuis 不能超过 %d 字节", maxNameLength)
	}
	if hasControl(building) {
		return validationFailedf("elcbuis 不能包含控制字符")
	}
	return nil
}

// hasControl 判断字符串是否包含控制字符.
func hasControl(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

// hasControlOrSpace 判断字符串是否包含控制字符或空白字符.
func hasControlOrSpace(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

// runeCount 返回字符串的 Unicode 字符数.
func runeCount(s string) int {
	return utf8.RuneCountInString(s)
}
