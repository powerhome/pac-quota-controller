package utils

import (
	"k8s.io/apimachinery/pkg/api/resource"
)

func ResourceMustParse(val string) resource.Quantity {
	return resource.MustParse(val)
}
