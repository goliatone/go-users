// Flash message auto-dismiss
document.addEventListener('DOMContentLoaded', function() {
    const flashMessages = document.querySelectorAll('.flash');

    flashMessages.forEach(function(flash) {
        // Auto-dismiss after 5 seconds
        setTimeout(function() {
            flash.style.opacity = '0';
            setTimeout(function() {
                flash.remove();
            }, 300);
        }, 5000);

        // Manual dismiss
        const closeBtn = flash.querySelector('.flash-close');
        if (closeBtn) {
            closeBtn.addEventListener('click', function() {
                flash.style.opacity = '0';
                setTimeout(function() {
                    flash.remove();
                }, 300);
            });
        }
    });

    // Confirmation modal for delete actions
    const deleteButtons = document.querySelectorAll('[data-confirm]');
    deleteButtons.forEach(function(btn) {
        btn.addEventListener('click', function(e) {
            const message = btn.getAttribute('data-confirm') || 'Are you sure?';
            if (!confirm(message)) {
                e.preventDefault();
            }
        });
    });
});
