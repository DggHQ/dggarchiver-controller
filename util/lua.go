package util

import (
	log "github.com/DggHQ/dggarchiver-logger"
	dggarchivermodel "github.com/DggHQ/dggarchiver-model"
	lua "github.com/yuin/gopher-lua"
	luar "layeh.com/gopher-luar"
)

type LuaResponse struct {
	Filled  bool
	Error   bool
	Message string
	Data    map[string]interface{}
}

func LuaCallReceiveFunction(L *lua.LState, vod *dggarchivermodel.YTVod) *LuaResponse {
	luaVOD := luar.New(L, vod)

	result := &LuaResponse{}
	L.SetGlobal("ReceiveResponse", luar.New(L, result))

	if err := L.CallByParam(lua.P{
		Fn:      L.GetGlobal("OnReceive"),
		NRet:    0,
		Protect: true,
	}, luaVOD); err != nil {
		log.Debugf("Wasn't able to access the \"OnReceive\" function of the Lua script, skipping: %s", err)
		return nil
	}

	if result.Filled {
		if result.Error {
			log.Errorf("Wasn't able to execute the \"OnReceive\" function of the Lua script: %s", result.Message)
			return nil
		}
	}

	return result
}

func LuaCallContainerFunction(L *lua.LState, vod *dggarchivermodel.YTVod, success bool) *LuaResponse {
	luaVOD := luar.New(L, vod)
	luaSuccess := luar.New(L, success)

	result := &LuaResponse{}
	L.SetGlobal("ContainerResponse", luar.New(L, result))

	if err := L.CallByParam(lua.P{
		Fn:      L.GetGlobal("OnContainer"),
		NRet:    0,
		Protect: true,
	}, luaVOD, luaSuccess); err != nil {
		log.Debugf("Wasn't able to access the \"OnContainer\" function of the Lua script, skipping: %s", err)
		return nil
	}

	if result.Filled {
		if result.Error {
			log.Errorf("Wasn't able to execute the \"OnContainer\" function of the Lua script: %s", result.Message)
			return nil
		}
	}

	return result
}
