// Copyright (c) 2026 Nawakarit
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License v3.0.
let lastMaster = "";

browser.webRequest.onBeforeRequest.addListener(
  (details) => {
    if (!details.url.includes("master.m3u8")) return;

    lastMaster = details.url;

    console.log(lastMaster);
  },
  {
    urls: ["<all_urls>"],
  },
);
