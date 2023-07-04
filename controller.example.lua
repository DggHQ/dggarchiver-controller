local http = require("http")
local json = require("json")
local inspect = require("inspect")

ContainerResponse = {}

local client = http.client()

local pushoverToken = "xxx"
local pushoverUser = "xxx"

local char_to_hex = function(c)
	return string.format("%%%02X", string.byte(c))
end

local function urlencode(url)
	if url == nil then
		return
	end
	url = url:gsub("\n", "\r\n")
	url = url:gsub("([^%w ])", char_to_hex)
	url = url:gsub(" ", "+")
	return url
end

function OnContainer(vod, success)
	local msg = "Successfully created a worker container"
	if (!success) then
		msg = "Wasn't able to create a worker container"
	end
	local vodTable = {
		Platform = vod.Platform,
		Downloader = vod.Downloader,
		ID = vod.ID,
		PlaybackURL = vod.PlaybackURL,
		PubTime = vod.PubTime,
		Title = vod.Title,
		StartTime = vod.StartTime,
		EndTime = vod.EndTime,
		Thumbnail = vod.Thumbnail,
		ThumbnailPath = vod.ThumbnailPath,
		Path = vod.Path,
		Duration = vod.Duration,
	}
	local pushoverJSON, err = json.encode(vodTable)
	if err then
		ContainerResponse.filled = true
		ContainerResponse.error = true
		ContainerResponse.message = err
		return
	end
	local urlParams = string.format("?token=%s&user=%s&message=%s&title=%s&priority=-2", pushoverToken, pushoverUser, urlencode(pushoverJSON),
		urlencode(msg))
	local request = http.request("POST", "https://api.pushover.net/1/messages.json" .. urlParams)
	local result, err = client:do_request(request)
	if err then
		error(err)
		ContainerResponse.filled = true
		ContainerResponse.error = true
		ContainerResponse.message = err
		return
	end
	if not (result.code == 200) then
		ContainerResponse.filled = true
		ContainerResponse.error = true
		ContainerResponse.message = tostring(inspect(result))
		return
	end
	ContainerResponse.filled = true
	ContainerResponse.error = false
	ContainerResponse.message = "Sent a notification to Pushover successfully"
end