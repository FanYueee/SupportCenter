/* Forms work without JavaScript; HTMX only improves navigation and updates. */
document.documentElement.classList.add('file-input-enhanced');
document.documentElement.classList.add('range-enhanced');
const english = document.documentElement.lang === 'en';
const tr = (chinese, translated) => english ? translated : chinese;
function updateDaySummary(schedule) {
  if (!schedule) return;
  const summary = schedule.querySelector('[data-day-summary]');
  if (!summary) return;
  if (!schedule.querySelector('input[type="checkbox"]')?.checked) { summary.textContent = tr('休息', 'Closed'); return; }
  const periods = [...schedule.querySelectorAll('.range-row')].map(row => {
    const inputs = row.querySelectorAll('input[type="time"]');
    return inputs[0]?.value && inputs[1]?.value ? `${inputs[0].value}–${inputs[1].value}` : '';
  }).filter(Boolean);
  summary.textContent = periods.join(tr('、', ', ')) || tr('未設定', 'Not set');
}
document.addEventListener('input', event => updateDaySummary(event.target.closest?.('.day-schedule')));
document.addEventListener('change', event => updateDaySummary(event.target.closest?.('.day-schedule')));
document.addEventListener('change', event => {
  if (event.target.matches?.('select[data-auto-submit]')) event.target.form?.requestSubmit();
});
document.addEventListener('click', event => {
  const row = event.target.closest?.('tr[data-ticket-row]');
  if (!row || event.defaultPrevented || event.button !== 0 || event.altKey ||
      event.target.closest('a, button, input, select, textarea, label')) return;
  const link = row.querySelector('a.ticket-title');
  if (!link) return;
  if (event.metaKey || event.ctrlKey || event.shiftKey) {
    window.open(link.href, '_blank', 'noopener');
  } else {
    link.click();
  }
});
document.addEventListener('click', event => {
  const add = event.target.closest?.('[data-range-add]');
  if (add) {
    const form = add.closest('form');
    const list = add.closest('.day-schedule')?.querySelector('[data-range-list]');
    const template = form?.querySelector('#range-row-template');
    if (!list || !template || list.children.length >= 8) return;
    const row = template.content.cloneNode(true);
    const inputs = row.querySelectorAll('input[type="time"]');
    inputs[0].name = `day_start_${list.dataset.day}`;
    inputs[1].name = `day_end_${list.dataset.day}`;
    list.append(row);
    add.disabled = list.children.length >= 8;
    list.lastElementChild?.querySelector('input')?.focus();
    updateDaySummary(add.closest('.day-schedule'));
    return;
  }
  const remove = event.target.closest?.('[data-range-remove]');
  if (remove) {
    const schedule = remove.closest('.day-schedule');
    remove.closest('.range-row')?.remove();
    const button = schedule?.querySelector('[data-range-add]');
    if (button) button.disabled = false;
    updateDaySummary(schedule);
  }
});
function updateRelativeTimes() {
  const now = Date.now();
  for (const label of document.querySelectorAll('[data-relative-time]')) {
    const elapsed = Math.max(0, now - new Date(label.dataset.relativeTime).getTime());
    if (!Number.isFinite(elapsed)) continue;
    const minutes = Math.floor(elapsed / 60000);
    label.textContent = minutes < 1 ? tr('剛剛', 'Just now') : minutes < 60 ? tr(`${minutes} 分鐘前`, `${minutes}m ago`)
      : minutes < 1440 ? tr(`${Math.floor(minutes / 60)} 小時前`, `${Math.floor(minutes / 60)}h ago`) : tr(`${Math.floor(minutes / 1440)} 天前`, `${Math.floor(minutes / 1440)}d ago`);
  }
}
function localizeTimes() {
  const pad = value => String(value).padStart(2, '0');
  for (const time of document.querySelectorAll('time[data-local-time]')) {
    const date = new Date(time.dateTime);
    if (Number.isNaN(date.getTime())) continue;
    const offset = -date.getTimezoneOffset();
    const zone = offset === 0 ? 'UTC' : `UTC${offset > 0 ? '+' : '-'}${Math.floor(Math.abs(offset) / 60)}${Math.abs(offset) % 60 ? ':' + pad(Math.abs(offset) % 60) : ''}`;
    const short = `${pad(date.getMonth() + 1)}/${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
    const full = `${date.getFullYear()}/${short}`;
    const mode = time.dataset.localTime;
    if (mode === 'date') time.textContent = `${date.getFullYear()}/${pad(date.getMonth() + 1)}/${pad(date.getDate())}`;
    else time.textContent = (mode === 'full' ? full : short) + (time.hasAttribute('data-timezone-inline') ? ` ${zone}` : '');
    time.title = `${full} ${zone}`;
  }
  for (const label of document.querySelectorAll('[data-timezone-label]')) label.textContent = tr('本地時間', 'Local time');
  updateRelativeTimes();
}
document.addEventListener('DOMContentLoaded', localizeTimes);
document.addEventListener('htmx:afterSwap', localizeTimes);
// A metadata save replaces the workspace. Keep the viewport instead of letting
// browser scroll anchoring follow the surviving footer to the end of the thread.
const savedScrollPositions = new WeakMap();
document.addEventListener('htmx:beforeSwap', event => {
  if (!event.detail.requestConfig?.elt?.matches('form[data-preserve-scroll]')) return;
  savedScrollPositions.set(event.detail.xhr, { x: scrollX, y: scrollY });
  document.documentElement.classList.add('preserving-scroll');
});
function restoreSavePosition(event, finished = false) {
  const position = savedScrollPositions.get(event.detail.xhr);
  if (!position) return;
  window.scrollTo({ left: position.x, top: position.y, behavior: 'instant' });
  if (finished) {
    savedScrollPositions.delete(event.detail.xhr);
    document.documentElement.classList.remove('preserving-scroll');
  }
}
document.addEventListener('htmx:afterSwap', event => restoreSavePosition(event));
document.addEventListener('htmx:afterSettle', event => restoreSavePosition(event, true));
document.addEventListener('click', event => {
  const link = event.target.closest?.('a[data-image-preview]');
  if (!link || event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
  const dialog = document.getElementById('image-dialog');
  if (!(dialog instanceof HTMLDialogElement) || !dialog.showModal) return;
  event.preventDefault();
  dialog.querySelector('[data-preview-name]').textContent = link.dataset.imageName || '';
  dialog.querySelector('[data-preview-image]').src = link.href;
  dialog.querySelector('[data-preview-open]').href = link.href;
  dialog.querySelector('[data-preview-download]').href = link.dataset.originalUrl || link.href;
  dialog.showModal();
});
document.addEventListener('click', event => {
  const dialog = document.getElementById('image-dialog');
  if (!dialog?.open) return;
  if (event.target.closest?.('[data-preview-close]') || event.target === dialog) dialog.close();
});
document.addEventListener('close', event => {
  if (event.target.id === 'image-dialog') event.target.querySelector('[data-preview-image]').removeAttribute('src');
}, true);
const maxUploadSize = 10 * 1024 * 1024;
const maxSourceSize = 30 * 1024 * 1024;
const fileStates = new WeakMap();
function isImage(file) {
  return ['image/png', 'image/jpeg', 'image/webp'].includes(file.type);
}
function summaryFor(files) {
  return files.length === 0 ? tr('未選擇檔案', 'No files selected')
    : files.length === 1 ? files[0].name : tr(`已選擇 ${files.length} 個檔案`, `${files.length} files selected`);
}
function showFileState(input, message, error = false) {
  input.setCustomValidity(error ? message : '');
  const summary = input.closest('.file-picker')?.querySelector('[data-file-summary]');
  if (summary) {
    summary.textContent = message;
    summary.classList.toggle('file-picker-error', error);
  }
}
async function processImage(file, workerURL) {
  const bitmap = await createImageBitmap(file);
  try {
    if (bitmap.width * bitmap.height > 24000000) throw new Error(tr('圖片像素過大', 'Image dimensions are too large'));
    const canvas = document.createElement('canvas');
    canvas.width = bitmap.width;
    canvas.height = bitmap.height;
    const context = canvas.getContext('2d', { willReadFrequently: true });
    if (!context) throw new Error(tr('無法讀取圖片', 'Cannot read image'));
    context.drawImage(bitmap, 0, 0);
    const pixels = context.getImageData(0, 0, canvas.width, canvas.height);
    const result = await new Promise((resolve, reject) => {
      const worker = new Worker(workerURL, { type: 'module' });
      const timer = setTimeout(() => { worker.terminate(); reject(new Error(tr('圖片處理逾時', 'Image processing timed out'))); }, 30000);
      worker.onmessage = ({ data }) => {
        clearTimeout(timer);
        worker.terminate();
        if (data.error) reject(new Error(data.error)); else resolve(data);
      };
      worker.onerror = () => {
        clearTimeout(timer);
        worker.terminate();
        reject(new Error(tr('圖片處理失敗', 'Image processing failed')));
      };
      worker.postMessage({ file, pixels: pixels.data, width: canvas.width, height: canvas.height }, [pixels.data.buffer]);
    });
    const name = result.bestType === 'image/webp' ? file.name.replace(/\.[^.]+$/, '') + '.webp' : file.name;
    const upload = result.best && result.best.byteLength < file.size
      ? new File([result.best], name, { type: result.bestType, lastModified: file.lastModified }) : file;
    return { upload, preview: new Blob([result.preview], { type: 'image/webp' }) };
  } finally {
    bitmap.close();
  }
}
document.addEventListener('change', async event => {
  const input = event.target;
  if (!(input instanceof HTMLInputElement) || input.type !== 'file' || !input.dataset.imageWorker) return;
  const previous = fileStates.get(input);
  if (previous) for (const url of previous.urls) URL.revokeObjectURL(url);
  const state = { urls: [], version: (previous?.version || 0) + 1 };
  fileStates.set(input, state);
  const selected = Array.from(input.files || []);
  const previews = input.closest('.upload-field')?.querySelector('[data-image-previews]');
  previews?.replaceChildren();
  if (selected.length > 3) return showFileState(input, tr('最多上傳 3 個檔案', 'Up to 3 files'), true);
  if (selected.some(file => file.size === 0)) return showFileState(input, tr('不能上傳空檔案', 'Empty files are not allowed'), true);
  if (selected.some(file => isImage(file) && file.size > maxSourceSize))
    return showFileState(input, tr('圖片原檔不可超過 30 MB', 'Original image cannot exceed 30 MB'), true);
  if (selected.some(file => !isImage(file) && file.size > maxUploadSize))
    return showFileState(input, tr('每個檔案不可超過 10 MB', 'Each file must be under 10 MB'), true);
  if (!selected.some(isImage)) {
    showFileState(input, selected.some(file => file.size > maxUploadSize) ? tr('每個檔案不可超過 10 MB', 'Each file must be under 10 MB') : summaryFor(selected), selected.some(file => file.size > maxUploadSize));
    return;
  }
  input.dataset.processing = 'true';
  showFileState(input, tr('圖片處理中…', 'Processing images…'), true);
  const output = [];
  let saved = 0;
  for (const file of selected) {
    let upload = file;
    let preview = null;
    if (isImage(file)) {
      try {
        ({ upload, preview } = await processImage(file, input.dataset.imageWorker));
      } catch (_) {
        preview = file;
      }
      if (fileStates.get(input) !== state) return;
      if (previews && preview) {
        const url = URL.createObjectURL(preview);
        state.urls.push(url);
        const card = document.createElement('div');
        card.className = 'image-preview';
        const image = document.createElement('img');
        image.src = url;
        image.alt = file.name;
        const label = document.createElement('span');
        label.textContent = file.name;
        card.append(image, label);
        previews.append(card);
      }
    }
    output.push(upload);
    saved += file.size - upload.size;
  }
  if (fileStates.get(input) !== state) return;
  const transfer = new DataTransfer();
  for (const file of output) transfer.items.add(file);
  input.files = transfer.files;
  delete input.dataset.processing;
  const error = output.some(file => file.size > maxUploadSize) ? tr('每個檔案不可超過 10 MB', 'Each file must be under 10 MB') : '';
  showFileState(input, error || (summaryFor(output) + (saved > 0 ? tr(` · 節省 ${(saved / 1024).toFixed(0)} KB`, ` · saved ${(saved / 1024).toFixed(0)} KB`) : '')), Boolean(error));
});
document.addEventListener('submit', event => {
  if (event.target.querySelector('input[type="file"][data-processing]')) {
    event.preventDefault();
    event.stopImmediatePropagation();
  }
}, true);
document.addEventListener('htmx:beforeCleanupElement', event => {
  const element = event.detail.elt;
  const inputs = element.matches?.('input[data-image-worker]') ? [element]
    : element.querySelectorAll?.('input[data-image-worker]') || [];
  for (const input of inputs) {
    const state = fileStates.get(input);
    if (state) for (const url of state.urls) URL.revokeObjectURL(url);
    fileStates.delete(input);
  }
});
document.addEventListener('htmx:responseError', function (event) {
  const box = document.getElementById('request-error');
  if (box) {
    box.textContent = event.detail.xhr.status === 413
      ? tr('附件超過上傳限制，請縮小檔案後再試一次。', 'Attachment is too large. Choose a smaller file and try again.')
      : tr('操作未完成。請重新整理頁面後再試一次；輸入內容仍保留在表單中。', 'Action failed. Refresh the page and try again; your form entries are still here.');
    const response = new DOMParser().parseFromString(event.detail.xhr.responseText, 'text/html');
    const message = response.querySelector('.login-card p');
    if (message && event.detail.xhr.status !== 413) box.textContent = message.textContent;
    box.hidden = false;
    box.focus({ preventScroll: true });
  }
  restoreSavePosition(event, true);
});
document.addEventListener('htmx:sendError', function () {
  const box = document.getElementById('request-error');
  if (box) { box.textContent = tr('連線中斷，請確認網路後重試。', 'Connection lost. Check your network and try again.'); box.hidden = false; }
});
