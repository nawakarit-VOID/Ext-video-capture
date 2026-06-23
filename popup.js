// Copyright (c) 2026 Nawakarit
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License v3.0.
async function loadSession() {
  const res = await browser.runtime.sendMessage({
    type: "GET_SESSION",
  });

  console.log(res);

  document.getElementById("master").value = res.session.master || "";

  document.getElementById("time").textContent = new Date(
    res.session.time,
  ).toLocaleString();

  document.getElementById("headers").textContent = JSON.stringify(
    res.session.headers,
    null,
    2,
  );

  document.getElementById("copy").addEventListener("click", async () => {
    const url = document.getElementById("master").value;

    await navigator.clipboard.writeText(url);

    alert("Copied");
  });
}

loadSession();

/**
 * {
  "Host": "app.akuma-stream.com",
  "User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:152.0) Gecko/20100101 Firefox/152.0",
  "Accept": "",
  "Accept-Language": "th,en-US;q=0.9,en;q=0.8",
  "Accept-Encoding": "gzip, deflate, br, zstd",
  "Connection": "keep-alive",
  "Referer": "https://app.akuma-stream.com/watch/4af2fa47-bc13-488b-b007-87904917c264",
  "Sec-Fetch-Dest": "empty",
  "Sec-Fetch-Mode": "cors",
  "Sec-Fetch-Site": "same-origin"
}
 */
