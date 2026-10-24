<?php

/*
 +-----------------------------------------------------------------------+
 | patrabahok_guard localization: English (US)                            |
 | License: GNU GPLv3+                                                    |
 +-----------------------------------------------------------------------+
*/

$labels = [];

// Banner
$labels['title_danger'] = 'This message looks like a phishing attempt';
$labels['title_warn'] = 'Be careful with this message';
$labels['dismiss'] = 'Dismiss';
$labels['verifiedsender'] = 'Verified sender';

// Reasons ($vars are filled in by the analysis)
$labels['r_userinfo'] = 'A link hides its real destination behind an "@" ($host).';
$labels['r_ip'] = 'A link points to a bare IP address ($host) instead of a website name.';
$labels['r_punycode'] = 'A link uses an encoded international name ($host) that can imitate another site.';
$labels['r_mixedscript'] = 'A link mixes alphabets in its address ($host) to imitate another site.';
$labels['r_brandlink'] = 'A link pretends to be $brand but goes to $host.';
$labels['r_lookalikelink'] = 'A link goes to $host, which imitates your own domain $own.';
$labels['r_shortener'] = 'A link uses a URL shortener ($host) that hides where it leads.';
$labels['r_mismatch'] = 'A link shows $shown but actually goes to $host.';
$labels['r_brandsender'] = 'The sender\'s domain $domain imitates $brand.';
$labels['r_lookalikesender'] = 'The sender\'s domain $domain imitates your own domain $own.';
$labels['r_punycodesender'] = 'The sender\'s domain $domain uses an encoded international name.';
$labels['r_nameaddress'] = 'The sender name shows $shown, but the message really comes from $real.';
$labels['r_brandname'] = 'The sender calls itself $brand but writes from $domain.';
$labels['r_replyto'] = 'Replies go to $domain, not to the sender\'s domain $from.';
$labels['r_dmarcfail'] = 'The message failed sender verification (DMARC) for $domain.';
$labels['r_spffail'] = 'The sending server is not authorized to send mail for $domain.';
$labels['r_rspamd'] = 'The spam filter flagged this message as phishing or spoofed ($symbols).';
$labels['r_spam'] = 'The spam filter marked this message as spam.';
$labels['r_lure'] = 'It uses pressure wording ("$phrase") together with links to other sites.';

// Link confirmation
$labels['linkdialogtitle'] = 'Open this link?';
$labels['linkdialogtext'] = 'This link looks suspicious. It does not go where it appears to.';
$labels['linkdialogreal'] = 'It really goes to:';
$labels['goback'] = 'Go back';
$labels['openanyway'] = 'Open anyway';

// Attachment scanning
$labels['scanpending'] = 'Scanning';
$labels['scanclean'] = 'Clean';
$labels['scaninfected'] = 'Virus';
$labels['scanerror'] = 'Not scanned';
$labels['scanskipped'] = 'Too large';
$labels['scanpendingtitle'] = 'Checking this attachment for viruses…';
$labels['scancleantitle'] = 'No virus found';
$labels['scaninfectedtitle'] = 'Virus found: $virus. This attachment is blocked.';
$labels['scanerrortitle'] = 'The virus scanner could not check this attachment';
$labels['scanskippedtitle'] = 'Too large to scan for viruses';
$labels['attblocked'] = 'This attachment contains a virus ($virus) and has been blocked.';
$labels['uploadblocked'] = 'Upload blocked: $virus detected';
$labels['blockedtitle'] = 'Attachment blocked';
$labels['blockedtext'] = 'The attachment "$name" contains a virus ($virus). It has been blocked to protect your device.';

// Categories
$labels['cat_primary'] = 'Primary';
$labels['cat_social'] = 'Social';
$labels['cat_promotions'] = 'Promotions';
$labels['cat_updates'] = 'Updates';
$labels['cat_forums'] = 'Forums';
$labels['prefcategories'] = 'Sort the inbox into categories';
$labels['setcatinvalid'] = 'Could not move the messages: invalid request.';
$labels['setcatfailed'] = 'Could not move all messages to that category.';
