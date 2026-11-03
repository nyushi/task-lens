// Timestamp is the data update time, never the browser refresh time.
function ageInfo(timestamp, nowSeconds, oldAfterSeconds = 3600) {
  if (!Number.isFinite(timestamp) || timestamp <= 0 || !Number.isFinite(new Date(timestamp * 1000).getTime())) {
    return {label: '更新時刻不明', old: false};
  }
  const seconds = nowSeconds - timestamp;
  if (seconds < 0) return {label: '未来の時刻', old: false};
  const label = seconds < 60 ? '1分未満前' : seconds < 3600 ? `${Math.floor(seconds / 60)}分前` : seconds < 86400 ? `${Math.floor(seconds / 3600)}時間前` : `${Math.floor(seconds / 86400)}日前`;
  return {label, old: seconds >= oldAfterSeconds};
}
