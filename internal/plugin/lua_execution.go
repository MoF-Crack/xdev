package plugin

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// bindCoroutineBudget makes each resumed thread use its resumer's current
// execution budget. NewThread inherits context only at creation time: retaining
// that child context would either leave top-level threads uncontrolled or keep
// them tied to a load/previous call which has already been canceled. Rebinding
// occurs on the VM owner goroutine before entering the child, never concurrently
// with its execution. Nested resumes therefore inherit the same active budget.
func bindCoroutineBudget(resumer, thread *lua.LState) {
	if ctx := resumer.Context(); ctx != nil {
		thread.SetContext(ctx)
	} else {
		thread.RemoveContext()
	}
}

// protectCoroutineResumes preserves the library's create/resume/wrap semantics,
// including yield values, status and wrapped error propagation. Constructors
// need no permanent thread registry: every executable entry rebinds its thread,
// even if a closure keeps it hidden from globals or the enclosing coroutine was
// itself created during an earlier call.
func protectCoroutineResumes(L *lua.LState) error {
	co, ok := L.GetGlobal(lua.CoroutineLibName).(*lua.LTable)
	if !ok {
		return fmt.Errorf("coroutine library is not a table")
	}
	resume, ok := co.RawGetString("resume").(*lua.LFunction)
	if !ok || !resume.IsG {
		return fmt.Errorf("coroutine.resume is not a native function")
	}
	wrap, ok := co.RawGetString("wrap").(*lua.LFunction)
	if !ok || !wrap.IsG {
		return fmt.Errorf("coroutine.wrap is not a native function")
	}
	co.RawSetString("resume", L.NewFunction(func(current *lua.LState) int {
		bindCoroutineBudget(current, current.CheckThread(1))
		return resume.GFunction(current)
	}))
	co.RawSetString("wrap", L.NewFunction(func(current *lua.LState) int {
		n := wrap.GFunction(current)
		wrapped := current.CheckFunction(-1)
		// gopher-lua v1.1.2's native wrap closure stores its thread in the
		// first upvalue and enters internal coResume directly. Guard this
		// dependency rather than silently losing cancellation after an upgrade.
		if !wrapped.IsG || len(wrapped.Upvalues) != 1 {
			current.RaiseError("unsupported coroutine.wrap closure")
			return 0
		}
		thread, ok := wrapped.Upvalues[0].Value().(*lua.LState)
		if !ok {
			current.RaiseError("coroutine.wrap closure has no thread")
			return 0
		}
		invoke := wrapped.GFunction
		wrapped.GFunction = func(resumer *lua.LState) int {
			bindCoroutineBudget(resumer, thread)
			return invoke(resumer)
		}
		return n
	}))
	return nil
}
