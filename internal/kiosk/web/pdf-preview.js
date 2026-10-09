/* Local PDF preview: fit a complete page in the visible frame, independently
   of print scaling. Render only visible pages to bound canvas memory usage. */
(function () {
  let library;
  window.KioskPDFPreview = {
    mount(host, url) {
      let disposed = false;
      let task;
      let pdf;
      let observer;
      let resizeObserver;
      let width = 0;
      let height = 0;
      const entries = [];
      const stack = document.createElement('div');
      stack.className = 'pdf-preview-stack';
      host.tabIndex = 0;
      host.replaceChildren(stack);
      const status = document.createElement('p');
      status.className = 'pdf-preview-status';
      status.textContent = 'Загружаем предпросмотр…';
      status.setAttribute('role', 'status');
      stack.append(status);

      async function draw(entry) {
        if (disposed || !entry.visible || !width || !height) return;
        const generation = ++entry.generation;
        if (entry.render) entry.render.cancel();
        const canvas = document.createElement('canvas');
        canvas.setAttribute('aria-label', 'Страница ' + entry.number);
        entry.box.replaceChildren(canvas);
        const scale = Math.min(width / entry.base.width, height / entry.base.height);
        const viewport = entry.page.getViewport({ scale });
        const ratio = Math.min(window.devicePixelRatio || 1, 2);
        canvas.width = Math.ceil(viewport.width * ratio);
        canvas.height = Math.ceil(viewport.height * ratio);
        canvas.style.width = viewport.width + 'px';
        canvas.style.height = viewport.height + 'px';
        try {
          entry.render = entry.page.render({ canvasContext: canvas.getContext('2d'), viewport,
            transform: ratio === 1 ? null : [ratio, 0, 0, ratio, 0, 0] });
          await entry.render.promise;
        } catch (error) {
          if (!disposed && generation === entry.generation && error.name !== 'RenderingCancelledException') {
            entry.box.textContent = 'Не удалось показать страницу ' + entry.number;
          }
        }
      }

      function resize() {
        const nextWidth = Math.max(1, host.clientWidth - 32);
        const nextHeight = Math.max(1, host.clientHeight - 24);
        if (width === nextWidth && height === nextHeight) return;
        width = nextWidth;
        height = nextHeight;
        for (const entry of entries) {
          entry.box.style.height = height + 'px';
          if (entry.visible) draw(entry);
        }
      }

      (async () => {
        try {
          library ||= import('/static/pdfjs/pdf.mjs');
          const lib = await library;
          if (disposed) return;
          lib.GlobalWorkerOptions.workerSrc = '/static/pdfjs/pdf.worker.mjs';
          task = lib.getDocument({ url, cMapUrl: '/static/pdfjs/cmaps/', cMapPacked: true,
            standardFontDataUrl: '/static/pdfjs/standard_fonts/', wasmUrl: '/static/pdfjs/wasm/' });
          pdf = await task.promise;
          if (disposed) return;
          status.remove();
          observer = new IntersectionObserver((items) => {
            for (const item of items) {
              const entry = entries[Number(item.target.dataset.index)];
              entry.visible = item.isIntersecting;
              if (entry.visible) draw(entry);
              else {
                ++entry.generation;
                if (entry.render) entry.render.cancel();
                entry.box.replaceChildren();
              }
            }
          }, { root: host });
          resizeObserver = new ResizeObserver(resize);
          resizeObserver.observe(host);
          resize();
          for (let number = 1; number <= pdf.numPages && !disposed; number++) {
            const page = await pdf.getPage(number);
            if (disposed) return;
            const box = document.createElement('div');
            box.className = 'pdf-preview-page';
            box.dataset.index = entries.length;
            box.style.height = height + 'px';
            const entry = { page, number, box, base: page.getViewport({ scale: 1 }), visible: false, generation: 0 };
            entries.push(entry);
            stack.append(box);
            observer.observe(box);
          }
        } catch (error) {
          if (disposed) return;
          console.warn('PDF preview failed', error);
          stack.replaceChildren();
          const frame = document.createElement('iframe');
          frame.title = 'Предпросмотр документа';
          frame.src = url + '#page=1&view=Fit&zoom=page-fit&toolbar=0&navpanes=0';
          frame.className = 'pdf-preview-fallback';
          stack.append(frame);
        }
      })();
      return () => {
        disposed = true;
        observer?.disconnect();
        resizeObserver?.disconnect();
        for (const entry of entries) entry.render?.cancel();
        if (task) task.destroy().catch(() => {});
      };
    },
  };
})();
