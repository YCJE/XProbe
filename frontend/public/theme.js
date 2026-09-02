// 主题预应用(先于 bundle 执行, 避免闪烁; 外置文件以通过 CSP script-src 'self')
(function () {
  var pref = localStorage.getItem("xprobe-theme") || "system";
  var dark =
    pref === "dark" ||
    (pref === "system" && matchMedia("(prefers-color-scheme: dark)").matches);
  if (dark) document.documentElement.classList.add("dark");
})();
