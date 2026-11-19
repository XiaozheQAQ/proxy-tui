package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
)

var contextType = reflect.TypeOf((*context.Context)(nil)).Elem()
var errorType = reflect.TypeOf((*error)(nil)).Elem()

// invokeFacade calls one exported Facade method by name. Only methods on the
// facade are reachable; arbitrary application symbols cannot be named.
func invokeFacade(ctx context.Context, target any, method string, args []json.RawMessage) (result any, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = nil
			err = fmt.Errorf("frontend method %s panicked: %v", method, recovered)
		}
	}()
	value := reflect.ValueOf(target)
	methodValue := value.MethodByName(method)
	if !methodValue.IsValid() {
		return nil, fmt.Errorf("unsupported operation %q", method)
	}
	kind := methodValue.Type()
	want := kind.NumIn()
	hasContext := want > 0 && kind.In(0) == contextType
	offset := 0
	if hasContext {
		offset = 1
	}
	if len(args) != want-offset {
		return nil, fmt.Errorf("operation %s expects %d argument(s), got %d", method, want-offset, len(args))
	}
	inputs := make([]reflect.Value, 0, want)
	if hasContext {
		inputs = append(inputs, reflect.ValueOf(ctx))
	}
	for i, raw := range args {
		targetType := kind.In(i + offset)
		argument := reflect.New(targetType)
		if err := json.Unmarshal(raw, argument.Interface()); err != nil {
			return nil, fmt.Errorf("decode %s arg %d: %w", method, i, err)
		}
		inputs = append(inputs, argument.Elem())
	}
	outputs := methodValue.Call(inputs)
	if len(outputs) > 0 {
		last := outputs[len(outputs)-1]
		if last.Type().Implements(errorType) && !last.IsNil() {
			return nil, last.Interface().(error)
		}
	}
	switch len(outputs) {
	case 0:
		return nil, nil
	case 1:
		if outputs[0].Type().Implements(errorType) {
			return nil, nil
		}
		return outputs[0].Interface(), nil
	default:
		return outputs[0].Interface(), nil
	}
}
