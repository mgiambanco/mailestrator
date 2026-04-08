package com.mailclient.ui.detail;

import android.os.Bundle;
import android.view.LayoutInflater;
import android.view.Menu;
import android.view.MenuInflater;
import android.view.MenuItem;
import android.view.View;
import android.view.ViewGroup;
import android.webkit.WebSettings;
import android.webkit.WebView;
import android.widget.TextView;

import androidx.annotation.NonNull;
import androidx.annotation.Nullable;
import androidx.core.view.MenuProvider;
import androidx.fragment.app.Fragment;
import androidx.lifecycle.Lifecycle;
import androidx.lifecycle.ViewModelProvider;
import androidx.recyclerview.widget.LinearLayoutManager;
import androidx.recyclerview.widget.RecyclerView;

import com.mailclient.R;
import com.mailclient.data.model.Account;
import com.mailclient.data.model.Email;
import com.mailclient.ui.viewmodel.MailViewModel;

public class EmailDetailFragment extends Fragment {

    private MailViewModel viewModel;
    private AttachmentAdapter attachmentAdapter;

    @Nullable @Override
    public View onCreateView(@NonNull LayoutInflater inflater, @Nullable ViewGroup container,
                             @Nullable Bundle savedInstanceState) {
        return inflater.inflate(R.layout.fragment_email_detail, container, false);
    }

    @Override
    public void onViewCreated(@NonNull View view, @Nullable Bundle savedInstanceState) {
        super.onViewCreated(view, savedInstanceState);
        viewModel = new ViewModelProvider(requireActivity()).get(MailViewModel.class);

        WebView webView     = view.findViewById(R.id.web_view);
        TextView tvPlain    = view.findViewById(R.id.tv_plain_body);
        TextView tvFrom     = view.findViewById(R.id.tv_from);
        TextView tvSubject  = view.findViewById(R.id.tv_subject_detail);
        TextView tvDate     = view.findViewById(R.id.tv_date);
        RecyclerView rvAtt  = view.findViewById(R.id.rv_attachments);
        View attachSection  = view.findViewById(R.id.section_attachments);

        // WebView: disable JS, restrict network access
        WebSettings ws = webView.getSettings();
        ws.setJavaScriptEnabled(false);
        ws.setBlockNetworkImage(true);
        ws.setBlockNetworkLoads(true);

        // Attachment list
        rvAtt.setLayoutManager(new LinearLayoutManager(requireContext()));
        attachmentAdapter = new AttachmentAdapter("", "", "");
        rvAtt.setAdapter(attachmentAdapter);

        // Delete menu
        requireActivity().addMenuProvider(new MenuProvider() {
            @Override public void onCreateMenu(@NonNull Menu menu, @NonNull MenuInflater inflater) {
                inflater.inflate(R.menu.menu_detail, menu);
            }
            @Override public boolean onMenuItemSelected(@NonNull MenuItem item) {
                if (item.getItemId() == R.id.action_delete_email) {
                    Email email = viewModel.selectedEmail.getValue();
                    if (email != null) {
                        viewModel.deleteEmail(email);
                        requireActivity().getSupportFragmentManager().popBackStack();
                    }
                    return true;
                }
                return false;
            }
        }, getViewLifecycleOwner(), Lifecycle.State.RESUMED);

        viewModel.selectedEmail.observe(getViewLifecycleOwner(), email -> {
            if (email == null) return;

            tvFrom.setText(email.fromAddr != null ? email.fromAddr : "");
            tvSubject.setText(email.subject != null && !email.subject.isEmpty()
                    ? email.subject : getString(R.string.no_subject));
            tvDate.setText(email.received_at != null ? email.received_at : "");

            // Prefer HTML body; fall back to plain text
            if (email.body_html != null && !email.body_html.isEmpty()) {
                webView.setVisibility(View.VISIBLE);
                tvPlain.setVisibility(View.GONE);
                webView.loadDataWithBaseURL(null, email.body_html, "text/html", "UTF-8", null);
            } else {
                webView.setVisibility(View.GONE);
                tvPlain.setVisibility(View.VISIBLE);
                tvPlain.setText(email.body_text != null ? email.body_text : "");
            }

            // Attachments
            if (email.attachments != null && !email.attachments.isEmpty()) {
                attachSection.setVisibility(View.VISIBLE);
                // Rebuild adapter with correct credentials
                Account acc = viewModel.selectedAccount.getValue();
                String token = acc != null ? acc.token : "";
                AttachmentAdapter newAdapter = new AttachmentAdapter(
                        email.account_id, email.id, token);
                newAdapter.setItems(email.attachments);
                rvAtt.setAdapter(newAdapter);
                attachmentAdapter = newAdapter;
            } else {
                attachSection.setVisibility(View.GONE);
            }

            // Mark badge updated (email read state may change via API later)
            viewModel.updateBadge();
        });
    }
}
