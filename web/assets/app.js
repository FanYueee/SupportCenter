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
function fileSize(bytes) {
  return bytes < 1024 * 1024 ? `${Math.max(1, Math.round(bytes / 1024))} KB` : `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
// Every chosen file shows at once: images as pictures, others by name, each
// with a button to take it out again.
function previewCard(input, file, index, state) {
  const card = document.createElement('div');
  card.className = 'image-preview';
  if (isImage(file)) {
    const url = URL.createObjectURL(file);
    state.urls.push(url);
    const image = document.createElement('img');
    image.src = url;
    image.alt = file.name;
    card.append(image);
  } else {
    const kind = document.createElement('div');
    kind.className = 'file-preview-kind';
    kind.textContent = (file.name.match(/\.([^.]+)$/)?.[1] || tr('檔案', 'File')).slice(0, 6).toUpperCase();
    card.append(kind);
  }
  const label = document.createElement('span');
  label.textContent = file.name;
  const size = document.createElement('small');
  size.textContent = fileSize(file.size);
  const remove = document.createElement('button');
  remove.type = 'button';
  remove.className = 'file-preview-remove';
  remove.textContent = '×';
  remove.setAttribute('aria-label', `${tr('移除', 'Remove')} ${file.name}`);
  remove.addEventListener('click', () => chooseFiles(input, state.originals.filter((_, i) => i !== index)));
  card.append(label, size, remove);
  return card;
}
// chooseFiles puts files in the input and handles them as if picked.
function chooseFiles(input, files) {
  const transfer = new DataTransfer();
  for (const file of files) transfer.items.add(file);
  input.files = transfer.files;
  input.dispatchEvent(new Event('change', { bubbles: true }));
}
document.addEventListener('change', async event => {
  const input = event.target;
  if (!(input instanceof HTMLInputElement) || input.type !== 'file' || !input.dataset.imageWorker) return;
  const previous = fileStates.get(input);
  if (previous) for (const url of previous.urls) URL.revokeObjectURL(url);
  const selected = Array.from(input.files || []);
  const state = { urls: [], originals: selected, version: (previous?.version || 0) + 1 };
  fileStates.set(input, state);
  const previews = input.closest('.upload-field')?.querySelector('[data-image-previews]');
  previews?.replaceChildren(...selected.map((file, index) => previewCard(input, file, index, state)));
  if (selected.length > 3) return showFileState(input, tr('最多上傳 3 個檔案', 'Up to 3 files'), true);
  if (selected.some(file => file.size === 0)) return showFileState(input, tr('不能上傳空檔案', 'Empty files are not allowed'), true);
  if (selected.some(file => isImage(file) && file.size > maxSourceSize))
    return showFileState(input, tr('圖片原檔不可超過 30 MB', 'Original image cannot exceed 30 MB'), true);
  if (selected.some(file => !isImage(file) && file.size > maxUploadSize))
    return showFileState(input, tr('每個檔案不可超過 10 MB', 'Each file must be under 10 MB'), true);
  if (!selected.some(isImage)) return showFileState(input, summaryFor(selected));
  input.dataset.processing = 'true';
  showFileState(input, tr('圖片處理中…', 'Processing images…'), true);
  const output = [];
  let saved = 0;
  for (const [index, file] of selected.entries()) {
    let upload = file;
    if (isImage(file)) {
      let preview = null;
      try {
        ({ upload, preview } = await processImage(file, input.dataset.imageWorker));
      } catch (_) {
        upload = file;
      }
      if (fileStates.get(input) !== state) return;
      // The original shows at once; the small WebP replaces it when ready.
      const image = previews?.children[index]?.querySelector('img');
      if (image && preview) {
        const url = URL.createObjectURL(preview);
        state.urls.push(url);
        image.src = url;
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
// Files dropped anywhere on the page join those already chosen: in the form
// they were dropped on, or else the page's attachment field.
function dropTarget(event) {
  if (!event.dataTransfer?.types?.includes('Files')) return null;
  const selector = 'input[type="file"][data-image-worker]';
  return event.target.closest?.('form')?.querySelector(selector) || document.querySelector(selector);
}
let dragDepth = 0;
function endDrag() {
  dragDepth = 0;
  document.documentElement.classList.remove('file-dragging');
}
document.addEventListener('dragenter', event => {
  if (!dropTarget(event)) return;
  dragDepth++;
  document.documentElement.classList.add('file-dragging');
});
document.addEventListener('dragleave', event => {
  if (dragDepth && --dragDepth === 0) endDrag();
});
document.addEventListener('dragover', event => {
  if (!dropTarget(event)) return;
  event.preventDefault();
  event.dataTransfer.dropEffect = 'copy';
});
document.addEventListener('dragend', endDrag);
document.addEventListener('drop', event => {
  endDrag();
  const input = dropTarget(event);
  if (!input || !event.dataTransfer.files.length) return;
  event.preventDefault();
  const current = fileStates.get(input)?.originals || Array.from(input.files || []);
  chooseFiles(input, [...current, ...event.dataTransfer.files]);
  input.closest('.upload-field')?.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
});
// Attachments upload when the form is sent; the bar shows how far along.
function uploadProgress(form) {
  const box = form?.querySelector?.('[data-upload-progress]');
  return box && form.querySelector('input[type="file"]')?.files.length ? box : null;
}
document.addEventListener('htmx:xhr:loadstart', event => {
  const box = uploadProgress(event.target);
  if (!box) return;
  box.querySelector('progress').value = 0;
  box.querySelector('[data-upload-percent]').textContent = tr('上傳中 0%', 'Uploading 0%');
  box.hidden = false;
});
document.addEventListener('htmx:xhr:progress', event => {
  const box = uploadProgress(event.target);
  if (!box || box.hidden || !event.detail.lengthComputable || !event.detail.total) return;
  // The response's own progress follows the upload's; never go backwards.
  const bar = box.querySelector('progress');
  bar.value = Math.max(bar.value, Math.round(event.detail.loaded / event.detail.total * 100));
  box.querySelector('[data-upload-percent]').textContent = bar.value < 100 ? tr(`上傳中 ${bar.value}%`, `Uploading ${bar.value}%`) : tr('處理中…', 'Processing…');
});
document.addEventListener('htmx:afterRequest', event => {
  const box = event.target.querySelector?.('[data-upload-progress]');
  if (box) box.hidden = true;
  // A check token works once; a failed submission needs a new one.
  if (!event.detail.successful && window.turnstile) {
    for (const widget of event.target.querySelectorAll?.('[data-turnstile][data-widget]') || []) window.turnstile.reset(widget.dataset.widget);
  }
});
// The formatting bar writes the Markdown that messages understand, so nobody
// has to know it: selected text is wrapped, or each selected line prefixed.
const inlineMarks = { bold: '**', italic: '*', strike: '~~', code: '`' };
const linePrefixes = { quote: /^> ?/, bullet: /^[-*+] /, number: /^\d+[.)] / };
function edited(field) {
  field.focus();
  field.dispatchEvent(new Event('input', { bubbles: true }));
}
function insertText(field, text) {
  field.focus();
  field.setRangeText(text, field.selectionStart, field.selectionEnd, 'end');
  edited(field);
}
function formatField(field, format, trigger = '#') {
  const { selectionStart: start, selectionEnd: end, value } = field;
  const selected = value.slice(start, end);
  if (format === 'mention') {
    insertText(field, (start > 0 && !/\s$/.test(value.slice(0, start)) ? ' ' : '') + trigger);
    return;
  }
  if (format === 'code' && selected.includes('\n')) {
    const before = start > 0 && value[start - 1] !== '\n' ? '\n' : '';
    field.setRangeText(`${before}\`\`\`\n${selected}\n\`\`\`\n`, start, end, 'end');
  } else if (inlineMarks[format]) {
    const mark = inlineMarks[format];
    const around = value.slice(start - mark.length, start) === mark && value.slice(end, end + mark.length) === mark &&
      !(format === 'italic' && (value[start - 2] === '*' || value[end + 1] === '*'));
    if (around) {
      field.setRangeText(selected, start - mark.length, end + mark.length, 'select');
    } else if (selected.length > 2 * mark.length && selected.startsWith(mark) && selected.endsWith(mark)) {
      field.setRangeText(selected.slice(mark.length, -mark.length), start, end, 'select');
    } else {
      field.setRangeText(mark + selected + mark, start, end, 'select');
      field.setSelectionRange(start + mark.length, end + mark.length);
    }
  } else if (linePrefixes[format]) {
    const lineStart = value.lastIndexOf('\n', start - 1) + 1;
    let lineEnd = value.indexOf('\n', end > start && value[end - 1] === '\n' ? end - 1 : end);
    if (lineEnd < 0) lineEnd = value.length;
    const pattern = linePrefixes[format];
    const lines = value.slice(lineStart, lineEnd).split('\n');
    const remove = lines.every(line => pattern.test(line));
    const prefix = i => format === 'quote' ? '> ' : format === 'bullet' ? '- ' : `${i + 1}. `;
    field.setRangeText(lines.map((line, i) => remove ? line.replace(pattern, '') : prefix(i) + line).join('\n'), lineStart, lineEnd, 'select');
    if (!remove && lines.length === 1 && start === end) field.setSelectionRange(start + prefix(0).length, start + prefix(0).length);
  }
  edited(field);
}
document.addEventListener('click', event => {
  const button = event.target.closest?.('[data-format]');
  const field = button?.closest('.composer')?.querySelector('textarea');
  if (field) formatField(field, button.dataset.format, button.dataset.trigger);
});
document.addEventListener('keydown', event => {
  const field = event.target;
  if (!(event.ctrlKey || event.metaKey) || event.altKey || !field.closest?.('.composer') || field.tagName !== 'TEXTAREA') return;
  const format = { b: 'bold', i: 'italic' }[event.key.toLowerCase()];
  if (!format) return;
  event.preventDefault();
  formatField(field, format);
});
// A canned reply goes in where the cursor is, or fills an empty box.
document.addEventListener('click', event => {
  const option = event.target.closest?.('[data-canned]');
  const field = option?.closest('.composer')?.querySelector('textarea');
  if (!field) return;
  const text = option.dataset.canned;
  if (!field.value.trim()) {
    field.value = text;
    field.setSelectionRange(text.length, text.length);
    edited(field);
  } else {
    const before = field.value.slice(0, field.selectionStart);
    insertText(field, (before && !before.endsWith('\n') ? '\n' : '') + text);
  }
  option.closest('details')?.removeAttribute('open');
});
document.addEventListener('input', event => {
  const filter = event.target.closest?.('[data-canned-filter]');
  if (!filter) return;
  const query = filter.value.trim().toLowerCase();
  const menu = filter.closest('.canned-menu');
  let shown = 0;
  for (const option of menu.querySelectorAll('[data-canned]')) {
    option.hidden = Boolean(query) && !option.textContent.toLowerCase().includes(query);
    if (!option.hidden) shown++;
  }
  menu.querySelector('[data-canned-empty]').hidden = shown > 0;
});
document.addEventListener('toggle', event => {
  if (event.target.matches?.('.canned-picker') && event.target.open) event.target.querySelector('[data-canned-filter]')?.focus();
}, true);
// Typing # in a message suggests tickets to mention, and a kind's trigger
// such as $ the customer's items to tag, narrowed as you type and picked with
// the arrow keys and Enter or Tab, or a tap.
const mentionTokens = new Map();
function mentionToken(triggers) {
  if (!mentionTokens.has(triggers)) {
    const set = triggers.replace(/[\\\]^-]/g, '\\$&');
    mentionTokens.set(triggers, new RegExp(`(?:^|[\\s(（「『【,，。、:：;；!！?？])([${set}])([^\\s${set}]{0,40})$`, 'u'));
  }
  return mentionTokens.get(triggers);
}
let mention = null;
function mentionAt(field) {
  if (field.selectionStart !== field.selectionEnd) return null;
  const match = field.value.slice(0, field.selectionStart).match(mentionToken('#' + (field.dataset.referenceTriggers || '')));
  return match && { trigger: match[1], query: match[2], start: field.selectionStart - match[2].length - 1 };
}
function closeMentions() {
  if (!mention) return;
  mention.request?.abort();
  clearTimeout(mention.timer);
  mention.menu.remove();
  mention.field.removeAttribute('aria-activedescendant');
  mention.field.removeAttribute('aria-controls');
  mention = null;
}
function mentionOptions() {
  return mention ? [...mention.menu.querySelectorAll('[data-insert]')] : [];
}
function moveMention(index, scroll = true) {
  const options = mentionOptions();
  if (!options.length) return mention.field.removeAttribute('aria-activedescendant');
  mention.active = (index + options.length) % options.length;
  options.forEach((option, i) => {
    option.id = `${mention.menu.id}-${i}`;
    option.setAttribute('aria-selected', String(i === mention.active));
  });
  mention.field.setAttribute('aria-activedescendant', options[mention.active].id);
  if (scroll) options[mention.active].scrollIntoView({ block: 'nearest' });
}
// Where a character sits in a textarea, found by laying the text out again
// in a hidden copy that wraps the same way.
const caretStyles = ['fontFamily', 'fontSize', 'fontWeight', 'fontStyle', 'letterSpacing', 'lineHeight', 'wordSpacing', 'tabSize', 'textIndent', 'textTransform', 'paddingTop', 'paddingRight', 'paddingBottom', 'paddingLeft'];
function caretPoint(field, index) {
  const style = getComputedStyle(field);
  const copy = document.createElement('div');
  for (const name of caretStyles) copy.style[name] = style[name];
  Object.assign(copy.style, {
    position: 'absolute', top: '0', left: '-9999px', visibility: 'hidden', boxSizing: 'content-box',
    width: `${field.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight)}px`,
    whiteSpace: 'pre-wrap', overflowWrap: 'break-word',
  });
  const mark = document.createElement('span');
  mark.textContent = field.value[index] || '#';
  copy.append(field.value.slice(0, index), mark, field.value.slice(index + 1));
  document.body.append(copy);
  const point = {
    left: field.offsetLeft + field.clientLeft + mark.offsetLeft - field.scrollLeft,
    top: field.offsetTop + field.clientTop + Math.min(Math.max(mark.offsetTop - field.scrollTop, 0), field.clientHeight - mark.offsetHeight),
    height: mark.offsetHeight,
  };
  copy.remove();
  return point;
}
// The list opens under the #, or above it when the screen has no room below.
function placeMentions() {
  if (!mention?.menu.childElementCount) return;
  const { field, menu, start } = mention;
  const caret = caretPoint(field, start);
  const box = menu.offsetParent.getBoundingClientRect();
  const screen = window.visualViewport?.height ?? innerHeight;
  const below = caret.top + caret.height + 4;
  const above = caret.top - menu.offsetHeight - 4;
  menu.style.left = `${Math.max(0, Math.min(caret.left - 12, menu.offsetParent.clientWidth - menu.offsetWidth))}px`;
  menu.style.top = `${box.top + below + menu.offsetHeight > screen - 8 && box.top + above >= 8 ? above : below}px`;
}
function showMentions(field, token) {
  if (mention?.field !== field) {
    closeMentions();
    const menu = document.createElement('div');
    menu.className = 'mention-menu';
    menu.id = `${field.id}-mentions`;
    menu.setAttribute('role', 'listbox');
    field.closest('.composer').append(menu);
    field.setAttribute('aria-controls', menu.id);
    mention = { field, menu, active: 0 };
  }
  const current = mention;
  current.start = token.start;
  const button = token.trigger === '#' ? null : field.closest('.composer').querySelector(`[data-trigger="${CSS.escape(token.trigger)}"]`);
  current.menu.setAttribute('aria-label', button?.getAttribute('aria-label') || tr('提及客服單', 'Mention a ticket'));
  clearTimeout(current.timer);
  current.timer = setTimeout(async () => {
    current.request?.abort();
    const request = current.request = new AbortController();
    const url = new URL(token.trigger === '#' ? field.dataset.mentionSearch : field.dataset.referenceSearch, location.origin);
    url.searchParams.set('q', token.query);
    if (token.trigger !== '#') url.searchParams.set('trigger', token.trigger);
    try {
      const response = await fetch(url, { signal: request.signal, headers: { 'HX-Request': 'true' } });
      if (!response.ok || mention !== current) return;
      const found = new DOMParser().parseFromString(await response.text(), 'text/html').body;
      current.menu.replaceChildren(...[...found.childNodes].map(node => document.importNode(node, true)));
      moveMention(0);
      placeMentions();
    } catch {}
  }, 120);
}
function pickMention(option) {
  const { field, start } = mention;
  field.setRangeText(`${option.dataset.insert} `, start, field.selectionStart, 'end');
  closeMentions();
  field.focus();
}
document.addEventListener('input', event => {
  const field = event.target;
  if (!field.matches?.('textarea[data-mention-search]')) return;
  const token = mentionAt(field);
  token ? showMentions(field, token) : closeMentions();
});
document.addEventListener('keydown', event => {
  if (!mention || event.target !== mention.field || event.isComposing) return;
  const options = mentionOptions();
  if (event.key === 'Escape') {
    event.preventDefault();
    closeMentions();
  } else if (options.length && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
    event.preventDefault();
    moveMention(mention.active + (event.key === 'ArrowDown' ? 1 : -1));
  } else if (options.length && (event.key === 'Enter' || event.key === 'Tab')) {
    event.preventDefault();
    pickMention(options[mention.active]);
  }
});
document.addEventListener('keyup', event => {
  if (mention && event.target === mention.field && ['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key) && !mentionAt(mention.field)) closeMentions();
});
// Picked on pointerdown, before the box loses focus; Safari does not focus buttons.
document.addEventListener('pointerdown', event => {
  if (!mention) return;
  const option = event.target.closest?.('.mention-menu [data-insert]');
  if (option && event.button === 0) {
    event.preventDefault();
    pickMention(option);
  } else if (!event.target.closest?.('.mention-menu') && event.target !== mention.field) {
    closeMentions();
  }
});
document.addEventListener('mousedown', event => {
  if (event.target.closest?.('.mention-menu')) event.preventDefault();
});
document.addEventListener('pointermove', event => {
  const option = mention && event.target.closest?.('.mention-menu [data-insert]');
  if (option) moveMention(mentionOptions().indexOf(option), false);
});
document.addEventListener('scroll', event => { if (event.target === mention?.field) placeMentions(); }, true);
addEventListener('resize', () => placeMentions());
document.addEventListener('focusout', event => {
  if (mention && event.target === mention.field) setTimeout(() => { if (mention && document.activeElement !== mention.field) closeMentions(); });
});
// Cloudflare's script calls this once loaded; the widget needs JavaScript anyway.
window.supportTurnstileReady = () => {
  for (const box of document.querySelectorAll('[data-turnstile]:not([data-widget])')) {
    const hint = box.closest('form')?.querySelector('[data-turnstile-hint]');
    box.dataset.widget = window.turnstile.render(box, {
      sitekey: box.dataset.sitekey, action: 'ticket', theme: 'auto', language: english ? 'en' : 'zh-tw',
      callback: () => { if (hint) hint.hidden = true; },
    });
  }
};
// Without a token the server turns the ticket away, so point to the check instead of sending.
document.addEventListener('submit', event => {
  const box = event.target.querySelector('[data-turnstile]');
  if (!box || event.target.querySelector('[name="cf-turnstile-response"]')?.value) return;
  event.preventDefault();
  event.stopImmediatePropagation();
  const hint = event.target.querySelector('[data-turnstile-hint]');
  if (hint) {
    hint.textContent = window.turnstile
      ? tr('請先完成上方的人機驗證，再送出客服單。', 'Complete the verification above, then submit the ticket.')
      : tr('人機驗證沒有載入。請重新整理頁面；若仍然看不到，請暫停阻擋內容的外掛或改用其他瀏覽器。', 'The verification did not load. Refresh the page; if it still does not appear, pause content blockers or try another browser.');
    hint.hidden = false;
  }
  box.scrollIntoView({ block: 'center', behavior: 'smooth' });
}, true);
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
// New replies chime once across this browser's tabs, unless turned off on the
// account page. Browsers play sound only after someone has used the page.
const soundKey = 'support-sound';
const heardKey = 'support-activity';
function soundOn() {
  try { return localStorage.getItem(soundKey) !== 'off'; } catch { return true; }
}
let audio = null;
function chime() {
  try {
    audio ||= new AudioContext();
    (audio.state === 'running' ? Promise.resolve() : audio.resume()).then(() => {
      if (audio.state !== 'running') return;
      [880, 1318.5].forEach((frequency, i) => {
        const tone = audio.createOscillator();
        const volume = audio.createGain();
        const at = audio.currentTime + i * 0.14;
        tone.frequency.value = frequency;
        volume.gain.setValueAtTime(0.0001, at);
        volume.gain.exponentialRampToValueAtTime(0.18, at + 0.02);
        volume.gain.exponentialRampToValueAtTime(0.0001, at + 0.35);
        tone.connect(volume).connect(audio.destination);
        tone.start(at);
        tone.stop(at + 0.36);
      });
    }, () => {});
  } catch {}
}
function unlockAudio() {
  try {
    audio ||= new AudioContext();
    if (audio.state === 'suspended') audio.resume().catch(() => {});
  } catch {}
}
document.addEventListener('pointerdown', unlockAudio, { once: true, capture: true });
document.addEventListener('keydown', unlockAudio, { once: true, capture: true });
// Returns the newest message this browser had already heard about.
function heardBefore(latest) {
  try {
    const heard = Number(localStorage.getItem(heardKey) || 0);
    if (latest > heard) localStorage.setItem(heardKey, String(latest));
    return heard;
  } catch { return 0; }
}
let latestActivity = Number(document.getElementById('activity-signal')?.dataset.activity || 0);
heardBefore(latestActivity);
document.addEventListener('htmx:load', event => {
  if (event.target.id === 'activity-signal') {
    const latest = Number(event.target.dataset.activity || 0);
    if (latest <= latestActivity) return;
    // A page that did not know the latest message only learns it.
    const known = latestActivity > 0;
    latestActivity = latest;
    if (heardBefore(latest) < latest && known && soundOn()) chime();
  }
  for (const toggle of event.target.querySelectorAll?.('[data-sound-toggle]') || []) toggle.checked = soundOn();
});
document.addEventListener('change', event => {
  if (!event.target.matches?.('[data-sound-toggle]')) return;
  try { localStorage.setItem(soundKey, event.target.checked ? 'on' : 'off'); } catch {}
});
document.addEventListener('click', event => {
  if (event.target.closest?.('[data-sound-test]')) chime();
});
for (const toggle of document.querySelectorAll('[data-sound-toggle]')) toggle.checked = soundOn();
