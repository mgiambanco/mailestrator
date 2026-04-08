package com.mailclient.ui.inbox;

import android.graphics.Canvas;
import android.graphics.Color;
import android.graphics.Paint;
import android.graphics.drawable.ColorDrawable;
import android.os.Bundle;
import android.view.LayoutInflater;
import android.view.Menu;
import android.view.MenuInflater;
import android.view.MenuItem;
import android.view.View;
import android.view.ViewGroup;
import android.widget.TextView;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.appcompat.widget.SearchView;
import androidx.core.view.MenuProvider;
import androidx.fragment.app.Fragment;
import androidx.lifecycle.Lifecycle;
import androidx.lifecycle.ViewModelProvider;
import androidx.recyclerview.widget.ItemTouchHelper;
import androidx.recyclerview.widget.LinearLayoutManager;
import androidx.recyclerview.widget.RecyclerView;
import androidx.swiperefreshlayout.widget.SwipeRefreshLayout;

import com.mailclient.MainActivity;
import com.mailclient.R;
import com.mailclient.data.model.Account;
import com.mailclient.data.model.Email;
import com.mailclient.ui.viewmodel.MailViewModel;

import java.util.ArrayList;
import java.util.List;
import java.util.stream.Collectors;

public class InboxFragment extends Fragment {

    private MailViewModel viewModel;
    private EmailAdapter adapter;
    private SwipeRefreshLayout swipeRefresh;
    private TextView tvEmpty;
    private String searchQuery = "";

    @Nullable @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container,
                             @Nullable Bundle savedInstanceState) {
        return inflater.inflate(R.layout.fragment_inbox, container, false);
    }

    @Override
    public void onViewCreated(@NonNull View view, @Nullable Bundle savedInstanceState) {
        super.onViewCreated(view, savedInstanceState);
        viewModel = new ViewModelProvider(requireActivity()).get(MailViewModel.class);

        swipeRefresh = view.findViewById(R.id.swipe_refresh);
        tvEmpty      = view.findViewById(R.id.tv_empty);
        RecyclerView rv = view.findViewById(R.id.rv_emails);

        rv.setLayoutManager(new LinearLayoutManager(requireContext()));
        adapter = new EmailAdapter();
        rv.setAdapter(adapter);

        adapter.setEmailClickListener(email -> {
            viewModel.selectedEmail.setValue(email);
            ((MainActivity) requireActivity()).navigateToDetail();
        });

        adapter.setLoadMoreClickListener(() -> {
            Account acc = viewModel.selectedAccount.getValue();
            if (acc != null) viewModel.loadMoreEmails(acc.id);
        });

        // Swipe-to-delete
        new ItemTouchHelper(new SwipeDeleteCallback()).attachToRecyclerView(rv);

        // Pull-to-refresh
        swipeRefresh.setOnRefreshListener(() -> {
            Account acc = viewModel.selectedAccount.getValue();
            if (acc != null) {
                viewModel.loadEmails(acc.id);
            } else {
                swipeRefresh.setRefreshing(false);
            }
        });

        // Menu (search + copy)
        requireActivity().addMenuProvider(new MenuProvider() {
            @Override public void onCreateMenu(@NonNull Menu menu, @NonNull MenuInflater inflater) {
                inflater.inflate(R.menu.menu_inbox, menu);

                MenuItem searchItem = menu.findItem(R.id.action_search);
                SearchView sv = (SearchView) searchItem.getActionView();
                sv.setQueryHint(getString(R.string.search_hint));
                sv.setOnQueryTextListener(new SearchView.OnQueryTextListener() {
                    @Override public boolean onQueryTextSubmit(String q) { return false; }
                    @Override public boolean onQueryTextChange(String q) {
                        searchQuery = q == null ? "" : q.trim();
                        updateList();
                        return true;
                    }
                });
            }
            @Override public boolean onMenuItemSelected(@NonNull MenuItem item) {
                if (item.getItemId() == R.id.action_copy_address) {
                    Account acc = viewModel.selectedAccount.getValue();
                    if (acc != null) {
                        android.content.ClipboardManager cm = (android.content.ClipboardManager)
                                requireContext().getSystemService(android.content.Context.CLIPBOARD_SERVICE);
                        cm.setPrimaryClip(android.content.ClipData.newPlainText("email", acc.address));
                        android.widget.Toast.makeText(requireContext(), R.string.copied, android.widget.Toast.LENGTH_SHORT).show();
                    }
                    return true;
                }
                return false;
            }
        }, getViewLifecycleOwner(), Lifecycle.State.RESUMED);

        // Observe emails
        viewModel.emailsByAccount.observe(getViewLifecycleOwner(), map -> {
            swipeRefresh.setRefreshing(false);
            updateList();
        });

        viewModel.hasMoreByAccount.observe(getViewLifecycleOwner(), hm -> updateLoadMore());
        viewModel.loadingMoreByAccount.observe(getViewLifecycleOwner(), lm -> updateLoadMore());

        viewModel.errorMessage.observe(getViewLifecycleOwner(), msg -> {
            swipeRefresh.setRefreshing(false);
            if (msg != null) {
                android.widget.Toast.makeText(requireContext(), msg, android.widget.Toast.LENGTH_LONG).show();
            }
        });

        viewModel.selectedAccount.observe(getViewLifecycleOwner(), account -> updateList());
    }

    private List<Email> currentEmails() {
        Account acc = viewModel.selectedAccount.getValue();
        if (acc == null) return new ArrayList<>();
        java.util.Map<String, List<Email>> map = viewModel.emailsByAccount.getValue();
        if (map == null) return new ArrayList<>();
        List<Email> all = map.getOrDefault(acc.id, new ArrayList<>());
        return all != null ? all : new ArrayList<>();
    }

    private void updateList() {
        List<Email> all = currentEmails();
        List<Email> filtered;
        if (searchQuery.isEmpty()) {
            filtered = all;
        } else {
            String q = searchQuery.toLowerCase();
            filtered = all.stream().filter(e ->
                    (e.subject != null && e.subject.toLowerCase().contains(q)) ||
                    (e.fromAddr != null && e.fromAddr.toLowerCase().contains(q)) ||
                    (e.body_text != null && e.body_text.toLowerCase().contains(q))
            ).collect(Collectors.toList());
        }
        adapter.submitList(new ArrayList<>(filtered));
        tvEmpty.setVisibility(filtered.isEmpty() ? View.VISIBLE : View.GONE);
        tvEmpty.setText(searchQuery.isEmpty() ? R.string.no_emails : R.string.no_results);
        updateLoadMore();
    }

    private void updateLoadMore() {
        Account acc = viewModel.selectedAccount.getValue();
        if (acc == null) { adapter.setShowLoadMore(false, false); return; }
        java.util.Map<String, Boolean> hm = viewModel.hasMoreByAccount.getValue();
        java.util.Map<String, Boolean> lm = viewModel.loadingMoreByAccount.getValue();
        boolean hasMore   = hm != null && Boolean.TRUE.equals(hm.get(acc.id));
        boolean loading   = lm != null && Boolean.TRUE.equals(lm.get(acc.id));
        // Only show load-more footer when not filtering
        adapter.setShowLoadMore(searchQuery.isEmpty() && hasMore, loading);
    }

    // ── Swipe-to-delete ───────────────────────────────────────────────────────

    private class SwipeDeleteCallback extends ItemTouchHelper.SimpleCallback {

        private final ColorDrawable bg = new ColorDrawable(Color.parseColor("#E53935"));
        private final Paint textPaint;

        SwipeDeleteCallback() {
            super(0, ItemTouchHelper.LEFT);
            textPaint = new Paint(Paint.ANTI_ALIAS_FLAG);
            textPaint.setColor(Color.WHITE);
            textPaint.setTextSize(40f);
        }

        @Override public boolean onMove(@NonNull RecyclerView rv,
                @NonNull RecyclerView.ViewHolder vh, @NonNull RecyclerView.ViewHolder t) { return false; }

        @Override public int getSwipeDirs(@NonNull RecyclerView rv,
                @NonNull RecyclerView.ViewHolder vh) {
            // Disable swipe on the load-more footer
            if (vh instanceof EmailAdapter.LoadMoreViewHolder) return 0;
            return super.getSwipeDirs(rv, vh);
        }

        @Override public void onSwiped(@NonNull RecyclerView.ViewHolder vh, int dir) {
            int pos = vh.getAdapterPosition();
            // getCurrentList() does not include the load-more sentinel
            List<Email> list = adapter.getCurrentList();
            if (pos < list.size()) {
                viewModel.deleteEmail(list.get(pos));
            }
        }

        @Override public void onChildDraw(@NonNull Canvas c, @NonNull RecyclerView rv,
                @NonNull RecyclerView.ViewHolder vh, float dX, float dY,
                int actionState, boolean isActive) {
            super.onChildDraw(c, rv, vh, dX, dY, actionState, isActive);
            View item = vh.itemView;
            bg.setBounds(item.getRight() + (int) dX, item.getTop(), item.getRight(), item.getBottom());
            bg.draw(c);
            String label = getString(R.string.delete);
            float textWidth = textPaint.measureText(label);
            c.drawText(label,
                    item.getRight() - textWidth - 48,
                    item.getTop() + (item.getHeight() + 40f) / 2,
                    textPaint);
        }
    }
}
